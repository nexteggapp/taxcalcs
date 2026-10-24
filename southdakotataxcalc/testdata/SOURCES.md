# Where these files came from

The finding in this package rests on what the South Dakota Department of
Revenue published. These files are text extracted from those documents. This
records which document, and how, so that anybody can redo it and diff the
result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and HTML pages with `h2t.py` below, run by Python 3.12.3.

| File | Source | sha256 of the download |
|---|---|---|
| `dor-sd-taxes.txt` | [South Dakota Department of Revenue, Taxes (individuals)](https://dor.sd.gov/individuals/taxes/) | `3cbecabd4ea7c05d32c75ee63dba5e6234628ba498c3ded52de5580c46aeb696` |

## Reproducing them

```bash
curl -L -o sd.html https://dor.sd.gov/individuals/taxes/
python3 h2t.py sd.html | awk '/^South Dakota is one of seven states/{print; exit}' > dor-sd-taxes.txt
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

The page's HTML changes between downloads, so a later download can have a different sha256 while the extract stays the same.
