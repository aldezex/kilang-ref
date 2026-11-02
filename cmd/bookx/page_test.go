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
	"slices"
	"strings"
	"testing"
	"time"
)

// pageHarness is the start of the Node programs that run the script of the page
// of the book (cmd/bookx/web/index.html) without a browser. It gives the script
// a marked that parses only what these tests write (fenced blocks, headings,
// pipe tables and plain lines, which become <p>) and reaches the script's own
// functions through one line it adds at the end of the script. Each test's
// program goes after it, as pageMain or viewsMain do: it reads its input from
// stdin and prints its answer as JSON.
const pageHarness = `
const fs = require("fs");
const vm = require("vm");

let src = fs.readFileSync(process.argv[2], "utf8");
const tail = /\n\s*boot\(\);\s*\}\)\(\);\s*$/;
if (!tail.test(src)) throw new Error("the script of the page does not end with boot(); })();");
src = src.replace(tail, "\nglobalThis.__page = { chapterHtml, chapterNavHtml, currentSection, placeAsides, renderChapter, renderWelcome, state };\n})();\n");

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
      const heading = /^(#{1,6}) (.*)$/.exec(line);
      if (heading) {
        out += m.renderer.heading(heading[2], heading[1].length, heading[2]);
      } else if (fence) {
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

// A stand-in for every element the script finds by id: it only keeps what the
// script writes.
const elements = {};
const fakeElement = () => ({ innerHTML: "", textContent: "", hidden: false, style: {}, children: [], querySelectorAll: () => [] });
const sandbox = {
  marked: fakeMarked(),
  hljs: { getLanguage: () => null, highlight: () => ({ value: "" }) },
  document: {
    documentElement: { scrollHeight: 0 },
    addEventListener() {},
    getElementById: (id) => (elements[id] = elements[id] || fakeElement()),
    querySelectorAll: () => [],
  },
  window: { addEventListener() {}, scrollTo() {}, matchMedia: () => ({ matches: false }) },
  location: { hash: "" },
  console,
};
vm.createContext(sandbox);
vm.runInContext(src, sandbox);
const page = sandbox.__page;
const input = JSON.parse(fs.readFileSync(0, "utf8"));
`

// pageMain prints chapterHtml's HTML for each Markdown text of its input.
const pageMain = `
process.stdout.write(JSON.stringify(input.map((md) => page.chapterHtml(md).html)));
`

// viewsMain shows one chapter whose title and milestone are the input, then the
// home page, and prints what each view wrote.
const viewsMain = `
(async () => {
  const c = Object.assign({ chapter: 1, file: "ch01.md", hours: 8, part: "I", tags: [] }, input);
  page.state.outline = { parts: [{ part: "I", title: "Fundamentos", hours: 8, milestones: "M1", chapters: [{ chapter: 1, title: c.title, hours: 8 }, { chapter: 2, title: c.title, hours: 8 }] }] };
  page.state.available.set(1, c);
  sandbox.fetch = async () => ({ ok: true, text: async () => "---\ntitle: x\n---\nTexto.\n" });
  await page.renderChapter(1);
  const views = { chapter: elements.page.innerHTML, toc: elements.parts.innerHTML };
  page.renderWelcome();
  views.home = elements.page.innerHTML;
  process.stdout.write(JSON.stringify(views));
})().catch((e) => { console.error(e); process.exit(1); });
`

// runPageScript runs the script of the page under pageHarness followed by main,
// gives it input as JSON and decodes what it prints into out. It skips the test
// when Node is not installed, as in the container of the CI: then only the tests
// of the page's source run.
func runPageScript(t *testing.T, main string, input, out any) {
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
	for p, data := range map[string][]byte{scriptPath: indexHTML[start+len(open) : end], harnessPath: []byte(pageHarness + main)} {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, harnessPath, scriptPath)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		t.Fatalf("the harness printed %q: %v", stdout.String(), err)
	}
}

// renderWithPage runs chapterHtml of the page on each Markdown text and returns
// the HTML it makes.
func renderWithPage(t *testing.T, texts ...string) []string {
	t.Helper()
	var htmls []string
	runPageScript(t, pageMain, texts, &htmls)
	if len(htmls) != len(texts) {
		t.Fatalf("the harness printed %d strings, want %d", len(htmls), len(texts))
	}
	return htmls
}

// sectionsMain prints what chapterHtml lists as the sections of each Markdown
// text, and the list the page makes of them.
const sectionsMain = `
process.stdout.write(JSON.stringify(input.map((md) => {
  const { html, sections } = page.chapterHtml(md);
  return { html, sections, nav: page.chapterNavHtml(sections, 5) };
})));
`

// currentMain prints the answer of currentSection for each case of its input.
const currentMain = `
process.stdout.write(JSON.stringify(input.map((c) => page.currentSection(c.tops, c.atBottom))));
`

// asidesMain runs placeAsides on a fake page with the children its input names,
// a tag and an optional class ("ASIDE.from"), for each of the steps of its
// input, true for a wide screen. It prints the children after each step.
const asidesMain = `
const nodes = input.children.map((name) => {
  const [tagName, cls] = name.split(".");
  return { name, tagName, classList: { contains: (c) => c === cls },
    get previousElementSibling() { const i = root.children.indexOf(this); return i > 0 ? root.children[i - 1] : null; },
    after(el) { root.children.splice(root.children.indexOf(el), 1); root.children.splice(root.children.indexOf(this) + 1, 0, el); } };
});
const root = { children: nodes,
  insertBefore(el, before) { this.children.splice(this.children.indexOf(el), 1); this.children.splice(this.children.indexOf(before), 0, el); } };
process.stdout.write(JSON.stringify(input.steps.map((wide) => {
  page.placeAsides(root, wide);
  return root.children.map((n) => n.name);
})));
`

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

	t.Run("titles_and_the_milestone_render_code_spans", func(t *testing.T) {
		var views struct{ Chapter, Toc, Home string }
		runPageScript(t, viewsMain, map[string]string{
			"title":     "El taller: `go test` & <b> y `a<b`",
			"milestone": "`ki --version`, sin `cerrar",
		}, &views)
		const title = "El taller: <code>go test</code> &amp; &lt;b&gt; y <code>a&lt;b</code>"
		const milestone = "<code>ki --version</code>, sin `cerrar"
		for _, c := range []struct{ view, got, want string }{
			{"h1", views.Chapter, "<h1>" + title + "</h1>"},
			{"milestone line", views.Chapter, `<p class="milestone"><span>Hito</span> ` + milestone + "</p>"},
			{"sidebar, open chapter", views.Toc, `<span class="num">1</span><span>` + title + "</span>"},
			{"sidebar, chapter still to come", views.Toc, `<span class="num">2</span><span>` + title + "</span>"},
			{"home card", views.Home, "<b>" + title + "</b><small>" + milestone + "</small>"},
		} {
			if !strings.Contains(c.got, c.want) {
				t.Errorf("%s: want %q in %.900q", c.view, c.want, c.got)
			}
		}
		if strings.Contains(views.Chapter, "Hito:") {
			t.Errorf("the milestone is no longer a chip: %.900q", views.Chapter)
		}
	})

	t.Run("the_list_of_sections_has_the_h2_and_h3_of_the_running_text", func(t *testing.T) {
		type section struct {
			ID    string `json:"id"`
			Level int    `json:"level"`
			HTML  string `json:"html"`
		}
		var got []struct {
			HTML     string    `json:"html"`
			Sections []section `json:"sections"`
			Nav      string    `json:"nav"`
		}
		md := "# Uno\n## Dos\n### Tres `a<b`\n#### Cuatro\n:::more Detalle\n## En un pliegue\n:::\n:::exercise\n### En un ejercicio\n:::\n## Cinco\n"
		runPageScript(t, sectionsMain, []string{md, "Solo texto.\n"}, &got)
		want := []section{{"sec-1", 2, "Dos"}, {"sec-2", 3, "Tres `a<b`"}, {"sec-3", 2, "Cinco"}}
		if len(got[0].Sections) != len(want) {
			t.Fatalf("want the sections %v, got %v", want, got[0].Sections)
		}
		for i, w := range want {
			if got[0].Sections[i] != w {
				t.Errorf("section %d: want %v, got %v", i, w, got[0].Sections[i])
			}
		}
		for _, w := range []string{
			"<h1>Uno</h1>", `<h2 id="sec-1">Dos</h2>`, `<h3 id="sec-2">Tres `, `<h2 id="sec-3">Cinco</h2>`,
			"<h4>Cuatro</h4>", "<h2>En un pliegue</h2>", "<h3>En un ejercicio</h3>",
		} {
			if !strings.Contains(got[0].HTML, w) {
				t.Errorf("want %q in %.600q", w, got[0].HTML)
			}
		}
		wantNav := `<h2>En este capítulo</h2><ol><li class="level-2"><a href="#ch05" data-sec="sec-1">Dos</a></li>` +
			`<li class="level-3"><a href="#ch05" data-sec="sec-2">Tres ` + "`a<b`" + `</a></li>` +
			`<li class="level-2"><a href="#ch05" data-sec="sec-3">Cinco</a></li></ol>`
		if got[0].Nav != wantNav {
			t.Errorf("want the list\n%s\ngot\n%s", wantNav, got[0].Nav)
		}
		if len(got[1].Sections) != 0 || got[1].Nav != "" {
			t.Errorf("a text with no headings has no list: %v, %q", got[1].Sections, got[1].Nav)
		}
	})

	t.Run("the_current_section_is_the_last_heading_that_went_past_the_line", func(t *testing.T) {
		line := 140 // SECTION_LINE of the page
		cases := []struct {
			name     string
			Tops     []int `json:"tops"`
			AtBottom bool  `json:"atBottom"`
			want     int
		}{
			{"none_past_the_line_yet", []int{line + 1, 900}, false, 0},
			{"exactly_on_the_line", []int{line, 900}, false, 0},
			{"second_on_the_line", []int{-500, line, 900}, false, 1},
			{"second_just_under_the_line", []int{-500, line + 1, 900}, false, 0},
			{"all_past_the_line", []int{-900, -500, -1}, false, 2},
			{"the_end_of_the_page_marks_the_last", []int{-500, 300, 500}, true, 2},
			{"no_sections", []int{}, false, -1},
			{"no_sections_at_the_end_of_the_page", []int{}, true, -1},
		}
		var got []int
		runPageScript(t, currentMain, cases, &got)
		for i, c := range cases {
			if got[i] != c.want {
				t.Errorf("%s: want %d, got %d", c.name, c.want, got[i])
			}
		}
	})

	t.Run("asides_go_before_their_paragraph_on_a_wide_screen_and_back", func(t *testing.T) {
		children := []string{
			"P.1", "ASIDE.from", "ASIDE.note", "H2", "ASIDE.note", "P.2", "UL", "ASIDE.from",
			"FIGURE", "ASIDE.from", "P.3", "ASIDE.bug", "OL", "ASIDE.note", "SECTION", "P.4", "BLOCKQUOTE", "ASIDE.from",
		}
		wideOrder := []string{
			"ASIDE.from", "ASIDE.note", "P.1", "H2", "ASIDE.note", "P.2", "ASIDE.from", "UL",
			"FIGURE", "ASIDE.from", "P.3", "ASIDE.bug", "ASIDE.note", "OL", "SECTION", "P.4", "ASIDE.from", "BLOCKQUOTE",
		}
		var got [][]string
		runPageScript(t, asidesMain, map[string]any{"children": children, "steps": []bool{false, true, true, false, true}}, &got)
		for i, want := range [][]string{children, wideOrder, wideOrder, children, wideOrder} {
			if !slices.Equal(got[i], want) {
				t.Errorf("step %d:\n want %v\n  got %v", i, want, got[i])
			}
		}
	})

	t.Run("the_grid_of_the_home_page_stays_on_the_home_page", func(t *testing.T) {
		// The fold of the cards has the class cards too: a bare .cards rule made it a
		// grid, with its table squeezed into one narrow track.
		if !bytes.Contains(indexHTML, []byte("\n.welcome .cards {")) || bytes.Contains(indexHTML, []byte("\n.cards {")) {
			t.Error("the grid of the cards of the home page has to be under .welcome")
		}
	})

	t.Run("the_opener_has_the_number_of_the_chapter_and_its_part", func(t *testing.T) {
		var views struct{ Chapter string }
		runPageScript(t, viewsMain, map[string]string{"title": "Uno", "milestone": "m"}, &views)
		for _, want := range []string{`<span>Capítulo</span><b class="chapter-num">1</b>`, `<span class="eyebrow">Parte I</span>`} {
			if !strings.Contains(views.Chapter, want) {
				t.Errorf("want %q in %.900q", want, views.Chapter)
			}
		}
	})

	t.Run("a_comparison_names_its_language_for_the_styles", func(t *testing.T) {
		cases := []struct{ arg, lang string }{{" Rust", "rust"}, {" TS", "ts"}, {" Python", "python"}, {"", ""}, {" A&B", "a&amp;b"}}
		var texts []string
		for _, c := range cases {
			texts = append(texts, ":::from"+c.arg+"\nTexto.\n:::\n")
		}
		for i, html := range renderWithPage(t, texts...) {
			if want := `<aside class="box from" data-lang="` + cases[i].lang + `">`; !strings.Contains(html, want) {
				t.Errorf(":::from%s: want %q in %.200q", cases[i].arg, want, html)
			}
		}
		for _, lang := range []string{"rust", "ts", "python"} {
			if s := `.box.from[data-lang="` + lang + `"] {`; !bytes.Contains(indexHTML, []byte(s)) {
				t.Errorf("the page does not contain %q", s)
			}
		}
	})

	t.Run("solutions_and_answers_take_their_colour_from_the_box_before_them", func(t *testing.T) {
		// Neither fold has a class of its own: the styles find them by what comes right before.
		html := renderWithPage(t, ":::exercise\nUno.\n:::\n:::solution\nDos.\n:::\n:::review\nTres.\n:::\n:::answer\nCuatro.\n:::\n")[0]
		html = strings.ReplaceAll(html, "\n", "")
		for _, want := range []string{`</section><details class="fold"><summary>Solución`, `</section><details class="fold"><summary>Respuesta`} {
			if !strings.Contains(html, want) {
				t.Errorf("want %q in %.500q", want, html)
			}
		}
		for _, s := range []string{"\nsection.exercise + details.fold {", "\nsection.review + details.fold {"} {
			if !bytes.Contains(indexHTML, []byte(s)) {
				t.Errorf("the page does not contain %q", s)
			}
		}
	})

	t.Run("the_styles_and_the_script_agree_on_the_margin_column", func(t *testing.T) {
		// The script moves the asides at the width where the styles float them.
		const query = "(min-width: 1380px)"
		for _, s := range []string{"@media " + query + " {", `MARGIN_QUERY = "` + query + `"`} {
			if !bytes.Contains(indexHTML, []byte(s)) {
				t.Errorf("the page does not contain %q", s)
			}
		}
	})
}
