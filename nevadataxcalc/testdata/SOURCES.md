# Where these files came from

The finding in this package rests on what the Nevada Legislature published.
These files are text extracted from those documents. This records which
document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and HTML pages with `h2t.py` below, run by Python 3.12.3.

| File | Source | sha256 of the download |
|---|---|---|
| `nv-const-art10-sec1-9.txt` | [Constitution of the State of Nevada, Article 10, Section 1(9)](https://www.leg.state.nv.us/Const/NvConst.html) | `726067b645f47709f00c2cf23ee6a514f7fc64381fd6c1f6eb0b1f48b31c51cc` |

## Reproducing them

```bash
curl -L -o nv-const.html https://www.leg.state.nv.us/Const/NvConst.html
python3 h2t.py nv-const.html windows-1252 \
  | awk '/^9\. No income tax shall be levied upon the/{f=1} f; /in the State\.$/{if(f)exit}' > nv-const-art10-sec1-9.txt
```

`h2t.py` prints an HTML page's visible text, one block per line:

```python
# h2t.py PAGE [ENCODING]: print an HTML page's visible text, one block per line.
import html.parser, sys

BLOCKS = {"p", "div", "br", "li", "tr", "td", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article"}

class Text(html.parser.HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.out, self.skip = [], 0
    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style", "noscript"):
            self.skip += 1
        if tag in BLOCKS:
            self.out.append("\n")
    def handle_endtag(self, tag):
        if tag in ("script", "style", "noscript") and self.skip:
            self.skip -= 1
    def handle_data(self, data):
        if not self.skip:
            self.out.append(data)

raw = open(sys.argv[1], "rb").read()
p = Text()
p.feed(raw.decode(sys.argv[2] if len(sys.argv) > 2 else "utf-8", errors="replace"))
for line in "".join(p.out).splitlines():
    line = " ".join(line.split())
    if line:
        print(line)
```

The page is encoded in Windows-1252, which `h2t.py` is told.
