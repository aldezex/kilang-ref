// bookx: not-in-book

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pageHarness is the Node program that runs the script of the page of the book
// (cmd/bookx/web/index.html) without a browser. It gives the script a marked
// that parses only what these tests write (fenced blocks, pipe tables and
// plain lines, which become <p>), reaches the script's own chapterHtml through
// one line it adds at the end of the script, and prints chapterHtml's answer
// for each Markdown text it reads from stdin as a JSON list of strings.
const pageHarness = `
const fs = require("fs");
const vm = require("vm");

let src = fs.readFileSync(process.argv[2], "utf8");
const tail = /\n\s*boot\(\);\s*\}\)\(\);\s*$/;
if (!tail.test(src)) throw new Error("the script of the page does not end with boot(); })();");
src = src.replace(tail, "\nglobalThis.__page = { chapterHtml };\n})();\n");

function fakeMarked() {
  const m = { renderer: null };
  m.Renderer = function () {};
  m.setOptions = (o) => { m.renderer = o.renderer; };
  m.parse = (text) => {
    const lines = text.split("\n");
    let out = "";
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      const fence = /^(` + "`{3,}" + `)(.*)$/.exec(line);
      if (fence) {
        const body = [];
        i++;
        while (i < lines.length && !lines[i].startsWith(fence[1])) body.push(lines[i++]);
        out += m.renderer.code(body.join("\n"), fence[2].trim());
      } else if (line.startsWith("|") && /^\|[-|]+\|$/.test(lines[i + 1] || "")) {
        const cells = (l) => l.split("|").slice(1, -1).map((c) => c.trim());
        const row = (l, tag) => "<tr>\n" + cells(l).map((c) => "<" + tag + ">" + c + "</" + tag + ">\n").join("") + "</tr>\n";
        const header = row(line, "th");
        let body = "";
        i += 2;
        while (i < lines.length && lines[i].startsWith("|")) body += row(lines[i++], "td");
        i--;
        out += m.renderer.table(header, body);
      } else if (line.trim() !== "") {
        out += "<p>" + line + "</p>";
      }
    }
    return out;
  };
  return m;
}

const sandbox = {
  marked: fakeMarked(),
  hljs: { getLanguage: () => null, highlight: () => ({ value: "" }) },
  document: { addEventListener() {}, getElementById: () => null, querySelectorAll: () => [] },
  window: { addEventListener() {}, scrollTo() {} },
  location: { hash: "" },
  console,
};
vm.createContext(sandbox);
vm.runInContext(src, sandbox);
const inputs = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(inputs.map((md) => sandbox.__page.chapterHtml(md).html)));
`

// renderWithPage runs chapterHtml of the page on each Markdown text and returns
// the HTML it makes. It skips the test when Node is not installed, as in the
// container of the CI: then only the tests of the page's source run.
func renderWithPage(t *testing.T, texts ...string) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the behaviour of the page is not tested here")
	}
	open, closing := []byte("<script>\n"), []byte("</script>")
	start, end := bytes.LastIndex(indexHTML, open), bytes.LastIndex(indexHTML, closing)
	if start < 0 || end < start {
		t.Fatal("the page has no inline <script>")
	}
	dir := t.TempDir()
	scriptPath, harnessPath := filepath.Join(dir, "page.js"), filepath.Join(dir, "harness.js")
	for p, data := range map[string][]byte{scriptPath: indexHTML[start+len(open) : end], harnessPath: []byte(pageHarness)} {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	input, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, harnessPath, scriptPath)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var htmls []string
	if err := json.Unmarshal(stdout.Bytes(), &htmls); err != nil || len(htmls) != len(texts) {
		t.Fatalf("the harness printed %q (%v), want %d strings", stdout.String(), err, len(texts))
	}
	return htmls
}

// numbered returns n lines, "linea 1" to "linea n", joined by newlines.
func numbered(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("linea %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// fileBlock is a Markdown block as `bookx render` writes it for {{file}}: n
// lines of code in a fence with the attributes the page reads.
func fileBlock(title string, n int) string {
	return "```go title=\"" + title + "\" tag=\"ch05-s1\" mode=\"file\"\n" + numbered(n) + "\n```"
}

func TestPage(t *testing.T) {
	const prefixFold = `<details class="fold long"><summary>`
	const suffixFold = `</figure></div></details>`

	t.Run("source_of_the_page_names_the_folds", func(t *testing.T) {
		// The folds are tested with Node below; this is what runs everywhere.
		// The header of the table of cards is the one bookx writes for {{cards}}.
		header := strings.Join(strings.Fields(strings.NewReplacer("|", " ").Replace(cardsTable(nil)[0])), " ")
		for _, s := range []string{
			header,
			"Tarjetas de test · ",
			" líneas</summary>",
			`case "more":`,
			`details class="fold more"`,
		} {
			if !bytes.Contains(indexHTML, []byte(s)) {
				t.Errorf("the page does not contain %q", s)
			}
		}
	})

	t.Run("code_blocks_over_30_lines_fold", func(t *testing.T) {
		const path = "internal/syntax/lexer.go"
		sizes := []int{1, 29, 30, 31, 100}
		var texts []string
		for _, n := range sizes {
			texts = append(texts, fileBlock(path, n))
		}
		for i, html := range renderWithPage(t, texts...) {
			n := sizes[i]
			if n <= 30 {
				if !strings.HasPrefix(html, `<figure class="code">`) || strings.Contains(html, "<details") {
					t.Errorf("%d lines: want the block open, a figure with no fold; got %.80q", n, html)
				}
				continue
			}
			wantPrefix := fmt.Sprintf("%s%s · %d líneas</summary><div class=\"fold-body\"><figure class=\"code\">", prefixFold, path, n)
			if !strings.HasPrefix(html, wantPrefix) || !strings.HasSuffix(html, suffixFold) || strings.Count(html, "<details") != 1 {
				t.Errorf("%d lines: want a closed fold that starts with %q and ends with %q; got %.200q", n, wantPrefix, suffixFold, html)
			}
			if strings.Contains(html, "<details open") {
				t.Errorf("%d lines: the fold has to start closed", n)
			}
		}
	})

	t.Run("a_blank_last_line_counts", func(t *testing.T) {
		// 30 lines plus one blank line before the closing fence are 31 lines.
		html := renderWithPage(t, "```go\n"+numbered(30)+"\n\n```")[0]
		if want := prefixFold + "go · 31 líneas</summary>"; !strings.HasPrefix(html, want) {
			t.Errorf("want a fold that starts with %q; got %.120q", want, html)
		}
	})

	t.Run("the_summary_names_the_file_or_the_language", func(t *testing.T) {
		texts := []string{
			fileBlock("a&b<c>.go", 40),
			"```text\n" + numbered(40) + "\n```",
			"```\n" + numbered(40) + "\n```",
		}
		want := []string{"a&amp;b&lt;c&gt;.go · 40 líneas", "text · 40 líneas", "texto · 40 líneas"}
		for i, html := range renderWithPage(t, texts...) {
			if w := prefixFold + want[i] + "</summary>"; !strings.HasPrefix(html, w) {
				t.Errorf("block %d: want a fold that starts with %q; got %.120q", i, w, html)
			}
		}
	})

	t.Run("a_reftest_keeps_its_fold", func(t *testing.T) {
		html := renderWithPage(t, "```go title=\"x_test.go\" tag=\"ch05-s1\" mode=\"test\" hint=\"T05.01 lexer\"\n"+numbered(40)+"\n```")[0]
		if !strings.HasPrefix(html, `<details class="fold reftest"><summary>Test de referencia · x_test.go · T05.01 lexer</summary>`) || strings.Count(html, "<details") != 1 {
			t.Errorf("a long reference test should keep its own fold and get no second one; got %.200q", html)
		}
	})

	t.Run("the_table_of_cards_folds", func(t *testing.T) {
		cards := func(rows int) string {
			s := "| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n"
			for i := 1; i <= rows; i++ {
				s += fmt.Sprintf("| T01.%02d | caso | porque | esperado | core |\n", i)
			}
			return s
		}
		others := []string{
			"| Código | Significado |\n|---|---|\n| E0101 | uno |\n",
			"| Id | Nombre |\n|---|---|\n| 1 | uno |\n",
			"| Id | Caso | Por qué | Esperado |\n|---|---|---|---|\n| T01.01 | a | b | c |\n",
			"| Id | Caso | Por qué | Esperado | Nivel | Autor |\n|---|---|---|---|---|---|\n| T01.01 | a | b | c | core | x |\n",
		}
		got := renderWithPage(t, append([]string{cards(1), cards(3), cards(40)}, others...)...)
		for i, n := range []int{1, 3, 40} {
			want := fmt.Sprintf(`<details class="fold cards"><summary>Tarjetas de test · %d casos</summary><div class="fold-body"><div class="table-wrap"><table>`, n)
			if !strings.HasPrefix(got[i], want) || !strings.HasSuffix(got[i], `</table></div></div></details>`) || strings.Count(got[i], "<details") != 1 {
				t.Errorf("%d cards: want a closed fold that starts with %q; got %.200q", n, want, got[i])
			}
		}
		for i := range others {
			if html := got[3+i]; !strings.HasPrefix(html, `<div class="table-wrap"><table>`) || strings.Contains(html, "<details") {
				t.Errorf("table %d is not the table of cards and has to stay open; got %.200q", i, html)
			}
		}
	})

	t.Run("more_is_a_closed_fold_with_its_title", func(t *testing.T) {
		got := renderWithPage(t,
			":::more Los códigos de salida\nTexto largo.\n:::\n",
			":::more A <b> & B\nTexto.\n:::\n",
			":::more La regla `==` y `a<b`\nTexto.\n:::\n",
			"Antes.\n\n:::more Uno\nUno.\n:::\n:::more Dos\nDos.\n:::\nDespués.\n",
		)
		for i, want := range []string{
			`<details class="fold more"><summary>Los códigos de salida</summary><div class="fold-body"><p>Texto largo.</p></div></details>`,
			`<details class="fold more"><summary>A &lt;b&gt; &amp; B</summary><div class="fold-body"><p>Texto.</p></div></details>`,
			`<details class="fold more"><summary>La regla <code>==</code> y <code>a&lt;b</code></summary><div class="fold-body"><p>Texto.</p></div></details>`,
			`<details class="fold more"><summary>Uno</summary><div class="fold-body"><p>Uno.</p></div></details>`,
		} {
			if !strings.Contains(got[i], want) {
				t.Errorf("text %d: want %q in %.400q", i, want, got[i])
			}
			if strings.Contains(got[i], "box") || strings.Contains(got[i], "<aside") || strings.Contains(got[i], "<details open") {
				t.Errorf("text %d: :::more must be a closed fold, not a box: %.400q", i, got[i])
			}
		}
		// Two in a row, with the text around them kept, in this order.
		in := []string{"<p>Antes.</p>", "<summary>Uno</summary>", "<summary>Dos</summary>", "<p>Después.</p>"}
		at := 0
		for _, s := range in {
			i := strings.Index(got[3][at:], s)
			if i < 0 {
				t.Fatalf("want %q after position %d in %.400q", s, at, got[3])
			}
			at += i
		}
	})

	t.Run("solutions_answers_and_more_keep_a_single_fold", func(t *testing.T) {
		long := fileBlock("a.go", 40)
		got := renderWithPage(t,
			":::exercise\nHaz algo.\n:::\n:::solution\n"+long+"\n:::\n",
			":::review\n¿Por qué?\n:::\n:::answer\n"+long+"\n:::\n",
			":::more Detalle\n"+long+"\n:::\n",
		)
		for i, open := range []string{
			`<details class="fold"><summary>Solución del ejercicio 1</summary>`,
			`<details class="fold"><summary>Respuesta</summary>`,
			`<details class="fold more"><summary>Detalle</summary>`,
		} {
			if !strings.Contains(got[i], open) || strings.Count(got[i], "<details") != 1 || !strings.Contains(got[i], `<figure class="code">`) {
				t.Errorf("text %d: want one fold (%s) around an open block; got %.400q", i, open, got[i])
			}
		}
	})

	t.Run("a_long_block_folds_again_after_a_closed_container", func(t *testing.T) {
		long := fileBlock("a.go", 40)
		got := renderWithPage(t, ":::solution\n"+long+"\n:::\n\n"+long+"\n")[0]
		if strings.Count(got, "<details") != 2 || !strings.Contains(got, prefixFold+"a.go · 40 líneas</summary>") {
			t.Errorf("want the solution fold and then a fold for the long block that follows it; got %.400q", got)
		}
	})

	t.Run("a_long_block_folds_inside_the_open_containers", func(t *testing.T) {
		long := fileBlock("a.go", 40)
		kinds := []string{"exercise", "review", "from Rust", "note", "bug"}
		var texts []string
		for _, k := range kinds {
			texts = append(texts, ":::"+k+"\n"+long+"\n:::\n")
		}
		for i, html := range renderWithPage(t, texts...) {
			if strings.Count(html, "<details") != 1 || !strings.Contains(html, prefixFold+"a.go · 40 líneas</summary>") {
				t.Errorf(":::%s: want the long block folded; got %.400q", kinds[i], html)
			}
		}
	})

	t.Run("solutions_and_answers_fold_as_before", func(t *testing.T) {
		html := renderWithPage(t, ":::exercise\nUno.\n:::\n:::solution\nCódigo.\n:::\n:::review\nPor qué.\n:::\n:::answer\nPorque.\n:::\n")[0]
		for _, want := range []string{
			`<details class="fold"><summary>Solución del ejercicio 1</summary><div class="fold-body"><p>Código.</p></div></details>`,
			`<details class="fold"><summary>Respuesta</summary><div class="fold-body"><p>Porque.</p></div></details>`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("want %q in %.400q", want, html)
			}
		}
	})
}
