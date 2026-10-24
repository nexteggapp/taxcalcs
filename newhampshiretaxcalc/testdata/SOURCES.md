# Where these files came from

The finding in this package rests on what the New Hampshire General Court
published. These files are text extracted from those documents. This records
which document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and HTML pages with `h2t.py` below, run by Python 3.12.3.

| File | Source | sha256 of the download |
|---|---|---|
| `rsa-77.txt` | [New Hampshire RSA chapter 77, Taxation of Incomes (repealed)](https://gc.nh.gov/rsa/html/V/77/77-mrg.htm) | `ee47f4063c84be2d86e84542932cc7b39e8ac767491c1b14382e2c8f0dd0d938` |
| `dra-nh-quoted-in-research.txt` | [New Hampshire Department of Revenue Administration, Interest & Dividends Tax, as quoted in `research/estimated-payments/nh.md`](https://www.revenue.nh.gov/taxes-glance/interest-dividends-tax) | not downloaded; see below |

## Reproducing them

```bash
curl -L -o 77-mrg.htm https://gc.nh.gov/rsa/html/V/77/77-mrg.htm
python3 h2t.py 77-mrg.htm > rsa-77.txt

# From the repository root:
grep -F -e 'W-2 wages."' -e 'beginning after December 31, 2024."' research/estimated-payments/nh.md \
  | sed -n '1,2p' > newhampshiretaxcalc/testdata/dra-nh-quoted-in-research.txt
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

## What could not be downloaded

The Department of Revenue Administration refuses scripted downloads. On
2026-10-20 both of these returned HTTP 403 "Access Denied" to curl:

- the Interest & Dividends Tax page,
  https://www.revenue.nh.gov/taxes-glance/interest-dividends-tax;
- TIR 2025-001, "Interest & Dividends Tax Repealed Effective January 1, 2025",
  https://www.revenue.nh.gov/sites/g/files/ehbemt736/files/documents/2025-001-technical-information-release-repeal.pdf.

The research file read both in a browser and verified their text
(`research/estimated-payments/nh.md`, Sources 1 and 5 and its Verification
section). `dra-nh-quoted-in-research.txt` is the two sentences it quotes from
the Interest & Dividends Tax page, copied from the research file at sha256
`b88a696d45451fc697da0669fc4bea23d8429d6911d91c2132db12034f9aa293`. They are
quotations of the department's page, not a download of it.

The repeal itself is checked against the statute, `rsa-77.txt`, which the
General Court serves without restriction.
