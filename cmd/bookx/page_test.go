// bookx: not-in-book

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
src = src.replace(tail, "\nglobalThis.__page = { chapterHtml, chapterNavHtml, currentSection, farthestSection, placeAsides, renderChapter, renderWelcome, stepSection, visibleSections, state };\n})();\n");

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
const jumps = [];
const fakeElement = (id) => ({ innerHTML: "", textContent: "", hidden: false, style: {}, children: [], querySelectorAll: () => [],
  getBoundingClientRect: () => ({ top: 0 }), scrollIntoView() { jumps.push(id); } });
// What the script listens to on the document, so a test can send it an event.
const listeners = {};
const sandbox = {
  marked: fakeMarked(),
  hljs: { getLanguage: () => null, highlight: () => ({ value: "" }) },
  document: {
    documentElement: { scrollHeight: 0 },
    addEventListener(type, fn) { listeners[type] = fn; },
    getElementById: (id) => (elements[id] = elements[id] || fakeElement(id)),
    querySelectorAll: () => [],
  },
  window: { addEventListener() {}, scrollTo() {}, matchMedia: () => ({ matches: false }) },
  location: { hash: "" },
  setTimeout() {},
  clearTimeout() {},
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
  page.state.outline = { parts: [
    { part: "I", title: "Fundamentos", hours: 8, milestones: "M1", chapters: [{ chapter: 1, title: c.title, hours: 8 }, { chapter: 2, title: c.title, hours: 8 }] },
    { part: "II", title: "El núcleo", hours: 8, milestones: "M2", chapters: [{ chapter: 3, title: c.title, hours: 8 }] },
  ] };
  page.state.available.set(1, c);
  sandbox.fetch = async () => ({ ok: true, text: async () => "---\ntitle: x\n---\nTexto.\n" });
  await page.renderChapter(1);
  const views = { chapter: elements.page.innerHTML, toc: elements.parts.innerHTML };
  page.renderWelcome();
  views.home = elements.page.innerHTML;
  views.tocHome = elements.parts.innerHTML;
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

// visibleMain prints the answer of visibleSections for each case of its input.
const visibleMain = `
process.stdout.write(JSON.stringify(input.map((c) => page.visibleSections(c.list, c.current))));
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

// farthestMain prints the answer of farthestSection for each case of its input.
const farthestMain = `
process.stdout.write(JSON.stringify(input.map((c) => page.farthestSection(c.list, c.visible, c.current))));
`

// stepMain prints the answer of stepSection for each case of its input.
const stepMain = `
process.stdout.write(JSON.stringify(input.map((c) => page.stepSection(c.tops, c.down, c.held))));
`

// keysMain presses each key of its input in a chapter with two sections, from a
// target that is or is not a field, and prints the sections each press jumped to.
const keysMain = `
page.state.current = 1;
page.state.sections = [{ id: "sec-1" }, { id: "sec-2" }];
elements["chapter-nav"] = Object.assign(fakeElement("chapter-nav"), { hidden: true });
for (const id of ["sec-1", "sec-2"]) elements[id] = Object.assign(fakeElement(id), { getBoundingClientRect: () => ({ top: 500 }) });
process.stdout.write(JSON.stringify(input.map((press) => {
  jumps.length = 0;
  let prevented = false;
  listeners.keydown(Object.assign({ preventDefault() { prevented = true; }, target: { closest: () => press.field ? {} : null } }, press));
  return { jumps: [...jumps], prevented };
})));
`

// endMain shows chapter 1 with the input as the other published chapters and
// prints the view.
const endMain = `
(async () => {
  page.state.outline = { parts: [{ part: "I", title: "Fundamentos", hours: 8, milestones: "M1", chapters: [{ chapter: 1, title: "Uno", hours: 8 }, { chapter: 2, title: "Dos ` + "`d`" + `", hours: 8 }] }] };
  page.state.available.set(1, { chapter: 1, file: "ch01.md", hours: 8, part: "I", title: "Uno", tags: [] });
  for (const c of input) page.state.available.set(c.chapter, Object.assign({ file: "x.md", hours: 8, part: "I", tags: [] }, c));
  sandbox.fetch = async () => ({ ok: true, text: async () => "---\ntitle: x\n---\nTexto.\n" });
  await page.renderChapter(1);
  process.stdout.write(JSON.stringify(elements.page.innerHTML));
})().catch((e) => { console.error(e); process.exit(1); });
`

// hueAndChroma returns the hue, in degrees, and the chroma, from 0 to 1, of a
// #rrggbb colour. A grey near white has a high HSL saturation but a low chroma.
func hueAndChroma(hex string) (float64, float64) {
	var r8, g8, b8 int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r8, &g8, &b8); err != nil {
		panic(err)
	}
	r, g, b := float64(r8)/255, float64(g8)/255, float64(b8)/255
	hi, lo := max(r, g, b), min(r, g, b)
	d := hi - lo
	if d == 0 {
		return 0, 0
	}
	var hue float64
	switch hi {
	case r:
		hue = math.Mod((g-b)/d+6, 6)
	case g:
		hue = (b-r)/d + 2
	default:
		hue = (r-g)/d + 4
	}
	return hue * 60, d
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
			want := fmt.Sprintf(`<details class="fold cards"><summary>Tarjetas de test · %d casos</summary><div class="fold-body"><ol class="cases"><li class="case">`, n)
			if !strings.HasPrefix(got[i], want) || !strings.HasSuffix(got[i], `</li></ol></div></details>`) || strings.Count(got[i], "<details") != 1 {
				t.Errorf("%d cards: want a closed fold that starts with %q; got %.200q", n, want, got[i])
			}
			if c := strings.Count(got[i], `<li class="case">`); c != n {
				t.Errorf("%d cards: got %d stacked items", n, c)
			}
			if strings.Contains(got[i], "<table") {
				t.Errorf("%d cards: the cards are stacked, not a table: %.200q", n, got[i])
			}
		}
		for i := range others {
			if html := got[3+i]; !strings.HasPrefix(html, `<div class="table-wrap"><table>`) || strings.Contains(html, "<details") {
				t.Errorf("table %d is not the table of cards and has to stay open; got %.200q", i, html)
			}
		}
	})

	t.Run("a_card_keeps_all_its_data_in_its_own_place", func(t *testing.T) {
		const row = "| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n| T05.07 | <code>lex</code> de un número | para no cortar <code>0x</code> | un token | stretch |\n"
		html := renderWithPage(t, row)[0]
		for _, want := range []string{
			`<span class="case-level">stretch</span>`,
			`<h4 class="case-head"><span class="case-id">T05.07</span><code>lex</code> de un número</h4>`,
			`<dt>Por qué</dt><dd>para no cortar <code>0x</code></dd>`,
			`<dt>Esperado</dt><dd>un token</dd>`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("want %q in %.500q", want, html)
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

	t.Run("a_long_section_gives_up_its_farthest_h3_first", func(t *testing.T) {
		type sec struct {
			Level int `json:"level"`
		}
		list := []sec{{2}, {3}, {3}, {3}, {3}, {2}}
		cases := []struct {
			name    string
			List    []sec  `json:"list"`
			Visible []bool `json:"visible"`
			Current int    `json:"current"`
			want    int
		}{
			{"from_the_first_h3_the_last_leaves", list, []bool{true, true, true, true, true, true}, 1, 4},
			{"from_the_last_h3_the_first_leaves", list, []bool{true, true, true, true, true, true}, 4, 1},
			{"on_a_tie_the_later_leaves", list, []bool{true, true, true, true, true, true}, 2, 4},
			{"the_ones_gone_stay_gone", list, []bool{true, true, true, true, false, true}, 1, 3},
			{"from_the_h2_above_them", list, []bool{true, true, true, true, true, true}, 0, 4},
			{"the_current_one_never_leaves", list, []bool{true, false, true, false, false, true}, 2, -1},
			{"an_h2_never_leaves", []sec{{2}, {2}}, []bool{true, true}, 0, -1},
		}
		var got []int
		runPageScript(t, farthestMain, cases, &got)
		for i, c := range cases {
			if got[i] != c.want {
				t.Errorf("%s: want %d, got %d", c.name, c.want, got[i])
			}
		}
	})

	t.Run("j_and_k_step_from_where_a_section_lands", func(t *testing.T) {
		landing := 72 // SECTION_LANDING of the page
		cases := []struct {
			name string
			Tops []int `json:"tops"`
			Down bool  `json:"down"`
			Held int   `json:"held"`
			want int
		}{
			{"down_from_the_top_of_the_chapter", []int{500, 900}, true, -1, 0},
			{"down_from_a_section_that_has_landed", []int{landing, 900}, true, -1, 1},
			{"down_from_the_middle_of_a_section", []int{-300, 900}, true, -1, 1},
			{"down_past_the_last", []int{-900, landing}, true, -1, -1},
			{"up_from_the_middle_of_a_section_goes_to_its_start", []int{-900, -300, 900}, false, -1, 1},
			{"up_from_a_section_that_has_landed", []int{-900, landing, 900}, false, -1, 0},
			{"up_from_the_first", []int{landing, 900}, false, -1, -1},
			{"down_during_a_jump_counts_from_its_target", []int{5000, 5900, 6500}, true, 0, 1},
			{"up_during_a_jump_counts_from_its_target", []int{-5000, -4000, -3000}, false, 2, 1},
			{"down_during_a_jump_to_the_last", []int{5000, 5900}, true, 1, -1},
			{"up_during_a_jump_to_the_first", []int{-5000, -4000}, false, 0, -1},
		}
		var got []int
		runPageScript(t, stepMain, cases, &got)
		for i, c := range cases {
			if got[i] != c.want {
				t.Errorf("%s: want %d, got %d", c.name, c.want, got[i])
			}
		}
	})

	t.Run("j_and_k_leave_fields_and_browser_shortcuts_alone", func(t *testing.T) {
		type press struct {
			Key   string `json:"key"`
			Field bool   `json:"field"`
			Ctrl  bool   `json:"ctrlKey"`
			Meta  bool   `json:"metaKey"`
			Alt   bool   `json:"altKey"`
			Shift bool   `json:"shiftKey"`
		}
		var got []struct {
			Jumps     []string `json:"jumps"`
			Prevented bool     `json:"prevented"`
		}
		ignored := []press{{Key: "j", Field: true}, {Key: "k", Field: true}, {Key: "j", Ctrl: true}, {Key: "j", Meta: true}, {Key: "j", Alt: true}, {Key: "J", Shift: true}, {Key: "j", Shift: true}, {Key: "a"}}
		runPageScript(t, keysMain, append(ignored, press{Key: "j"}), &got)
		for i, p := range ignored {
			if len(got[i].Jumps) != 0 || got[i].Prevented {
				t.Errorf("%+v: want the key left alone, got %+v", p, got[i])
			}
		}
		if last := got[len(ignored)]; !slices.Equal(last.Jumps, []string{"sec-1"}) || !last.Prevented {
			t.Errorf("j: want a jump to the first section, got %+v", last)
		}
	})

	t.Run("the_air_over_a_section_stands_before_its_heading", func(t *testing.T) {
		// A heading that clears a tall aside loses its margin, so the air is an element of its own.
		html := renderWithPage(t, "Uno.\n## Dos\n### Tres\n")[0]
		if !strings.Contains(html, `<div class="section-break"></div><h2 id="sec-1">Dos</h2>`) {
			t.Errorf("want the break just before the h2 in %.300q", html)
		}
		if strings.Contains(html, `<div class="section-break"></div><h3`) {
			t.Errorf("an h3 has no break: %.300q", html)
		}
	})

	t.Run("a_section_lands_above_the_line_that_makes_it_current", func(t *testing.T) {
		for _, s := range []string{"const SECTION_LANDING = 72;", ".page > h2, .page > h3 { scroll-margin-top: 72px; }", "const SECTION_LINE = 140;"} {
			if !bytes.Contains(indexHTML, []byte(s)) {
				t.Errorf("the page does not contain %q", s)
			}
		}
	})

	t.Run("a_new_page_starts_at_its_top_at_once_and_only_jumps_are_smooth", func(t *testing.T) {
		// With a smooth html, a plain scrollTo would run up from the end of the last chapter.
		if !bytes.Contains(indexHTML, []byte(`behavior: "instant"`)) || bytes.Contains(indexHTML, []byte("window.scrollTo(0, 0)")) {
			t.Error("a new page has to reach its top with an instant scroll")
		}
		motion := bytes.Index(indexHTML, []byte("@media (prefers-reduced-motion: no-preference) {"))
		smooth := bytes.Index(indexHTML, []byte("scroll-behavior: smooth"))
		if motion < 0 || smooth < motion || bytes.Count(indexHTML, []byte("scroll-behavior:")) != 1 {
			t.Error("smooth scrolling has to live under prefers-reduced-motion: no-preference, and only there")
		}
		if bytes.Contains(indexHTML, []byte("scroll-snap")) {
			t.Error("nothing snaps against the wheel")
		}
	})

	t.Run("the_end_of_a_chapter_shows_the_way_on", func(t *testing.T) {
		var next, coming string
		runPageScript(t, endMain, []map[string]any{{"chapter": 2, "title": "Dos `d`", "milestone": "m `x`"}}, &next)
		runPageScript(t, endMain, []map[string]any{}, &coming)
		for _, want := range []string{
			`<a class="end-next" href="#ch02">`,
			`Sigue con el capítulo 2`,
			`<span class="end-next-title">Dos <code>d</code></span>`,
			`<span class="end-next-note">m <code>x</code></span>`,
			`id="mark-read"`,
		} {
			if !strings.Contains(next, want) {
				t.Errorf("want %q in %.900q", want, next)
			}
		}
		if !strings.Contains(coming, `El capítulo 2 está en preparación`) || !strings.Contains(coming, `<span class="end-next-title">Dos <code>d</code></span>`) || strings.Contains(coming, `href="#ch02"`) {
			t.Errorf("a chapter still being written is named and has no link: %.900q", coming)
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

	t.Run("the_opener_is_plain_text_with_the_number_and_the_part", func(t *testing.T) {
		tags := func(n int) []string {
			ts := []string{"ch01"}
			for i := 1; i <= n; i++ {
				ts = append(ts, fmt.Sprintf("ch01-s%d", i))
			}
			return ts
		}
		for _, c := range []struct {
			sessions int
			facts    string
		}{{3, "8 h · 3 sesiones"}, {1, "8 h · 1 sesión"}, {0, "8 h"}} {
			var views struct{ Chapter string }
			runPageScript(t, viewsMain, map[string]any{"title": "Uno", "milestone": "m", "tags": tags(c.sessions)}, &views)
			for _, want := range []string{
				`<span class="eyebrow">Parte I · Fundamentos</span>`,
				`<b class="chapter-num" aria-label="Capítulo 1">1</b>`,
				`<p class="facts">` + c.facts + `</p>`,
			} {
				if !strings.Contains(views.Chapter, want) {
					t.Errorf("want %q in %.900q", want, views.Chapter)
				}
			}
			if strings.Contains(views.Chapter, "chip") {
				t.Errorf("the facts of the opener are plain text: %.900q", views.Chapter)
			}
		}
	})

	t.Run("only_the_part_of_the_current_chapter_is_open_in_the_contents", func(t *testing.T) {
		var views struct{ Toc, TocHome string }
		runPageScript(t, viewsMain, map[string]any{"title": "Uno", "milestone": "m", "part": "II", "chapter": 3}, &views)
		// The chapter stands in part II and is the first one to read, so both views open that part.
		for view, toc := range map[string]string{"chapter": views.Toc, "home": views.TocHome} {
			if !strings.Contains(toc, `<details class="part" name="toc-parts" data-part="II" open>`) {
				t.Errorf("%s: want part II open in %.600q", view, toc)
			}
			if !strings.Contains(toc, `<details class="part" name="toc-parts" data-part="I"><summary`) {
				t.Errorf("%s: want part I closed in %.600q", view, toc)
			}
			if !strings.Contains(toc, `title="Parte I · Fundamentos"`) {
				t.Errorf("%s: a closed part cuts its name, so its summary has the whole name as a title: %.600q", view, toc)
			}
		}
	})

	t.Run("the_list_of_sections_shows_the_h3_of_the_current_h2_only", func(t *testing.T) {
		type sec struct {
			Level int `json:"level"`
		}
		list := []sec{{3}, {2}, {3}, {3}, {2}, {2}, {3}}
		cases := []struct {
			name    string
			List    []sec `json:"list"`
			Current int   `json:"current"`
			want    []bool
		}{
			{"an_h3_before_any_h2", list, 0, []bool{true, true, false, false, true, true, false}},
			{"on_an_h2", list, 1, []bool{false, true, true, true, true, true, false}},
			{"on_its_second_h3", list, 3, []bool{false, true, true, true, true, true, false}},
			{"on_an_h2_with_no_h3", list, 4, []bool{false, true, false, false, true, true, false}},
			{"on_the_last", list, 6, []bool{false, true, false, false, true, true, true}},
			{"no_sections", []sec{}, -1, []bool{}},
		}
		var got [][]bool
		runPageScript(t, visibleMain, cases, &got)
		for i, c := range cases {
			if !slices.Equal(got[i], c.want) {
				t.Errorf("%s: want %v, got %v", c.name, c.want, got[i])
			}
		}
	})

	t.Run("comparisons_share_one_style_and_keep_their_language_in_the_markup", func(t *testing.T) {
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
		if bytes.Contains(indexHTML, []byte(".box.from[data-lang")) {
			t.Error("a comparison box is told apart by its label, not by a style per language")
		}
	})

	t.Run("the_palette_is_red_and_neutrals_and_dark_is_the_same_twice", func(t *testing.T) {
		start, end := bytes.Index(indexHTML, []byte("<style>")), bytes.Index(indexHTML, []byte("</style>"))
		if start < 0 || end < start {
			t.Fatal("the page has no <style>")
		}
		css := string(indexHTML[start:end])
		hexes := regexp.MustCompile(`#[0-9a-fA-F]{6}\b`).FindAllString(css, -1)
		if len(hexes) == 0 {
			t.Fatal("the page has no colours")
		}
		for _, h := range hexes {
			if hue, chroma := hueAndChroma(h); chroma > 0.1 && hue > 30 && hue < 330 {
				t.Errorf("%s is neither a neutral nor a red (hue %.0f, chroma %.2f)", h, hue, chroma)
			}
		}
		// The two dark blocks hold the same tokens: one follows the system and one follows an explicit choice.
		block := func(from string) string {
			i := strings.Index(css, from)
			if i < 0 {
				t.Fatalf("the styles do not contain %q", from)
			}
			rest := css[i+len(from):]
			j := strings.Index(rest, "}")
			return strings.Join(strings.Fields(rest[:j]), " ")
		}
		if a, b := block(`:root:not([data-theme="light"]) {`), block(`:root[data-theme="dark"] {`); a != b {
			t.Errorf("the dark blocks differ:\n%s\n%s", a, b)
		}
	})

	t.Run("solutions_and_answers_take_their_icon_from_the_box_before_them", func(t *testing.T) {
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
