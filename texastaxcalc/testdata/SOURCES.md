# Where these files came from

The finding in this package rests on what the Texas Legislature and the Office
of the Governor published. These files are text extracted from those documents.
This records which document, and how, so that anybody can redo it and diff the
result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and HTML pages with `h2t.py` below, run by Python 3.12.3.

| File | Source | sha256 of the download |
|---|---|---|
| `hjr-38-enrolled.txt` | [Texas Legislature, H.J.R. No. 38 (86th Legislature, 2019), enrolled](https://capitol.texas.gov/tlodocs/86R/billtext/pdf/HJ00038E.pdf) | `2555a29792a5873cd1e48bb04f7d3ff00fd05ca1274ce20577f9031caa3d6c3d` |
| `gov-tx-proclamation-2019.txt` | [Office of the Texas Governor, proclamation of the results of the November 5, 2019 constitutional amendment election, December 4, 2019](https://gov.texas.gov/news/post/governor-abbott-issues-proclamation-declaring-the-results-of-the-recent-constitutional-amendment-election) | `24b06dfdca6a6af0b3e68082b56a7f82dd59f5a8721f8554c04758ee6f75d385` |

## Reproducing them

```bash
curl -L -o HJ00038E.pdf https://capitol.texas.gov/tlodocs/86R/billtext/pdf/HJ00038E.pdf
pdftotext -layout HJ00038E.pdf - | sed -E 's/^ *[0-9]+ +//' > hjr-38-enrolled.txt

curl -L -o proclamation.html https://gov.texas.gov/news/post/governor-abbott-issues-proclamation-declaring-the-results-of-the-recent-constitutional-amendment-election
python3 h2t.py proclamation.html \
  | awk '/^WHEREAS, ten proposed amendments/{print} /^WHEREAS, the tabulation and total of the votes/{print} /^PROPOSITION No\. 4,/{print; exit}' \
  > gov-tx-proclamation-2019.txt
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

The resolution's PDF numbers every line; `sed` removes the numbers so a sentence reads across lines. Its odd `A` characters stand for spaces in the PDF's font and are left as extracted. The codified constitution on statutes.capitol.texas.gov is built by script in the browser, so the enrolled resolution and the proclamation that it was adopted are checked in instead.
