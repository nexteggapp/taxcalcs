// A small Markdown renderer for the documents these pages show, such as the
// taxlisp specification: headings, paragraphs, nested lists, tables, fenced
// code and inline code, emphasis, links and the little TeX the spec uses.

/** A heading, for a table of contents. */
export interface Heading {
  level: number;
  text: string;
  id: string;
}

/**
 * Links a document's terms: a glossary term written in brackets, [figure],
 * and a type name written as code, `ReturnInputs`. Each returns the link's
 * HTML, or undefined to leave the text as it is.
 */
export interface Linker {
  term(text: string): string | undefined;
  code(text: string): string | undefined;
}

export interface Rendered {
  html: string;
  headings: Heading[];
}

export function escape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/** An id for a heading: lowercase words joined by hyphens. */
export function headingId(text: string): string {
  return text
    .toLowerCase()
    .replace(/`/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

const tex: Record<string, string> = { '\\to': '→', '-\\infty': '−∞', '+\\infty': '+∞', '\\infty': '∞' };

// The linker of the document being rendered.
let linker: Linker | undefined;

/** Renders one line of Markdown, linking its terms with link. */
export function inline(s: string, link: Linker | undefined = linker): string {
  const code: string[] = [];
  let out = s.replace(/`([^`]+)`/g, (_, c: string) => {
    code.push(link?.code(c) ?? `<code>${escape(c)}</code>`);
    return `\u0000${code.length - 1}\u0000`;
  });
  out = escape(out)
    .replace(/&lt;br&gt;/g, '<br>')
    .replace(/\$([^$]+)\$/g, (_, t: string) => tex[t.trim()] ?? `<i>${t.replace(/\\l?dots/g, '…').replace(/_(\w)/g, '<sub>$1</sub>')}</i>`)
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>')
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, t: string, u: string) =>
      /^https?:/.test(u) ? `<a href="${u}" rel="noopener">${t}</a>` : t,
    )
    .replace(/\[([^\]]+)\](?!\()/g, (all, t: string) => link?.term(t) ?? all);
  return out.replace(/\u0000(\d+)\u0000/g, (_, i: string) => code[Number(i)]);
}

/** Splits a table row on the pipes outside inline code. */
function cells(row: string): string[] {
  const out: string[] = [];
  let cur = '';
  let inCode = false;
  for (const ch of row.trim().replace(/^\||\|$/g, '')) {
    if (ch === '`') inCode = !inCode;
    if (ch === '|' && !inCode) {
      out.push(cur.trim());
      cur = '';
    } else cur += ch;
  }
  out.push(cur.trim());
  return out;
}

/** Colours taxlisp: comments, strings, money, keywords and declaring forms. */
export function highlightLisp(src: string): string {
  const re = /(;[^\n]*)|(#usd"[^"]*")|("(?:[^"\\]|\\.)*")|(:[A-Za-z_][\w-]*)|(\((?:def[\w-]*|deffigures?|use-proto|line|part|attach|bind|set|result|from|law)\b)/g;
  let out = '';
  let last = 0;
  for (let m = re.exec(src); m; m = re.exec(src)) {
    out += escape(src.slice(last, m.index));
    const [t] = m;
    if (m[1]) out += `<span class="hl-comment">${escape(t)}</span>`;
    else if (m[2]) out += `<span class="hl-money">${escape(t)}</span>`;
    else if (m[3]) out += `<span class="hl-string">${escape(t)}</span>`;
    else if (m[4]) out += `<span class="hl-keyword">${escape(t)}</span>`;
    else out += `(<span class="hl-form">${escape(t.slice(1))}</span>`;
    last = m.index + t.length;
  }
  return out + escape(src.slice(last));
}

function codeBlock(lang: string, body: string): string {
  const html = lang === 'lisp' ? highlightLisp(body) : escape(body);
  return `<pre class="code"${lang ? ` data-lang="${escape(lang)}"` : ''}><code>${html}</code></pre>`;
}

interface Item {
  indent: number;
  ordered: boolean;
  html: string[];
}

export function renderMarkdown(src: string, link?: Linker): Rendered {
  linker = link;
  try {
    return render(src);
  } finally {
    linker = undefined;
  }
}

function render(src: string): Rendered {
  const lines = src.replace(/\r/g, '').split('\n');
  const out: string[] = [];
  const headings: Heading[] = [];
  let para: string[] = [];
  let items: Item[] = [];

  const flushPara = () => {
    if (para.length) out.push(`<p>${inline(para.join(' '))}</p>`);
    para = [];
  };
  // Nests the collected items by indentation.
  const flushList = () => {
    if (!items.length) return;
    const stack: { indent: number; ordered: boolean }[] = [];
    let html = '';
    for (const it of items) {
      while (stack.length && it.indent < stack[stack.length - 1].indent) {
        html += `</li></${stack.pop()!.ordered ? 'ol' : 'ul'}>`;
      }
      if (!stack.length || it.indent > stack[stack.length - 1].indent) {
        stack.push({ indent: it.indent, ordered: it.ordered });
        html += `<${it.ordered ? 'ol' : 'ul'}><li>`;
      } else html += '</li><li>';
      html += it.html.join('');
    }
    while (stack.length) html += `</li></${stack.pop()!.ordered ? 'ol' : 'ul'}>`;
    out.push(html);
    items = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = /^(\s*)```(\w*)\s*$/.exec(line);
    if (fence) {
      const pad = fence[1].length;
      const body: string[] = [];
      for (i++; i < lines.length && !/^\s*```\s*$/.test(lines[i]); i++) body.push(lines[i].slice(pad));
      const block = codeBlock(fence[2], body.join('\n'));
      flushPara();
      if (items.length && pad > 0) items[items.length - 1].html.push(block);
      else {
        flushList();
        out.push(block);
      }
      continue;
    }
    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      flushPara();
      flushList();
      const level = h[1].length;
      const id = headingId(h[2]);
      headings.push({ level, text: h[2].replace(/`/g, ''), id });
      out.push(`<h${level} id="${id}">${inline(h[2])}</h${level}>`);
      continue;
    }
    if (/^\s*\|/.test(line)) {
      flushPara();
      flushList();
      const rows: string[][] = [];
      for (; i < lines.length && /^\s*\|/.test(lines[i]); i++) rows.push(cells(lines[i]));
      i--;
      const [head, , ...body] = rows;
      out.push(
        '<div class="table-wrap"><table class="spec-table"><thead><tr>' +
          head.map((c) => `<th>${inline(c)}</th>`).join('') +
          '</tr></thead><tbody>' +
          body.map((r) => '<tr>' + r.map((c) => `<td>${inline(c)}</td>`).join('') + '</tr>').join('') +
          '</tbody></table></div>',
      );
      continue;
    }
    const li = /^(\s*)([-*]|\d+\.)\s+(.*)$/.exec(line);
    if (li) {
      flushPara();
      items.push({ indent: li[1].length, ordered: /\d/.test(li[2]), html: [inline(li[3])] });
      continue;
    }
    if (/^\s*$/.test(line)) {
      flushPara();
      // A list continues past a blank line when the next line is indented.
      if (items.length && i + 1 < lines.length && !/^\s+\S/.test(lines[i + 1]) && !/^\s*([-*]|\d+\.)\s/.test(lines[i + 1])) {
        flushList();
      }
      continue;
    }
    if (/^---+\s*$/.test(line)) {
      flushPara();
      flushList();
      continue;
    }
    if (items.length && /^\s+\S/.test(line)) {
      items[items.length - 1].html.push(' ' + inline(line.trim()));
      continue;
    }
    flushList();
    para.push(line.trim());
  }
  flushPara();
  flushList();
  return { html: out.join('\n'), headings };
}
