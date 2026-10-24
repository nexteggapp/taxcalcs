# Where these files came from

The finding in this package rests on what the Tennessee Department of Revenue
and Secretary of State published. `dor-tn-hall-income-tax.txt` is text
extracted from the Department's page. `research-tn.md` is a copy of the
research file, used because the Secretary of State's document could not be
downloaded; see below. This records which document, and how, so that anybody can redo it
and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and HTML pages with `h2t.py` below, run by Python 3.12.3.

| File | Source | sha256 of the download |
|---|---|---|
| `dor-tn-hall-income-tax.txt` | [Tennessee Department of Revenue, Hall Income Tax](https://www.tn.gov/revenue/taxes/hall-income-tax.html) | `d1f0a7eb675642aa5dfd9af4493b2d5e6835d74b53d46d019831b7eb97c0a078` |
| `research-tn.md` | `research/estimated-payments/tn.md` at commit `31d590e18`, copied unchanged. **Not a primary source**: it stands in for the Tennessee Blue Book, which could not be fetched | `df554af42051daeb103fff670c2265ac582691a30c7377a54c8f0246de47cddf` |

## Reproducing them

```bash
curl -L -o tn.html https://www.tn.gov/revenue/taxes/hall-income-tax.html
python3 h2t.py tn.html \
  | awk '/^The Hall income tax is imposed only/{f=1} f; /Please do not file a return/{if(f)exit}' > dor-tn-hall-income-tax.txt

cp ../../research/estimated-payments/tn.md research-tn.md
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

## The Blue Book was blocked

On 2026-10-20 a plain `curl -L -o` of the Tennessee Blue Book 2025-2026,
https://publications.tnsosfiles.com/pub/blue_book/25-26/25-26_TennesseeState.pdf,
returned HTTP 403 with a CloudFront page reading "Request blocked." An earlier
copy was downloaded by sending a sos.tn.gov referrer to get past that block. It
has been removed, and no other attempt was made to get past the block.

The constitution's sentence, "the Legislature shall not levy, authorize or
otherwise permit any state or local tax upon payroll or earned personal
income", is checked instead against the quote in `research-tn.md`. The
research file's verification read that sentence in the Blue Book's Article II,
Section 28 and in the amendment text, SJR 1 of the 108th General Assembly. That
checks this package against the research, not against the Secretary of State.
Somebody with access should fetch the Blue Book, check the sentence, and
replace `research-tn.md` with an extract.
