# Where these files came from

Every Utah figure in this package is checked against a document the Utah State
Tax Commission or the Utah Legislature published. These files are text
extracted from those documents. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0, and the Utah Code pages with `h2t.py` below, run by Python
3.12.3. `T` is `https://files.tax.utah.gov/tax/forms` and `C` is
`https://le.utah.gov/xcode/Title59`.

| File | Source | sha256 of the download |
|---|---|---|
| `tc-40inst-2025-p4-5.txt` | [TC-40, Utah Individual Income Tax Instructions, 2025](https://files.tax.utah.gov/tax/forms/current/tc-40inst.pdf) (`T/current/tc-40inst.pdf`), pages 4 and 5 | `9ea2823d81494434fb4a3d77be1ad103042ba4bdd1462dcfdca33f8a2fdcddda` |
| `tc-40inst-2025-p13.txt` | the same PDF, page 13, Penalties and Interest | same |
| `tc-546.txt` | [TC-546, Individual Income Tax Prepayment Coupon (Rev. 10/26)](https://files.tax.utah.gov/tax/forms/current/tc-546.pdf) (`T/current/tc-546.pdf`), reading order | `8c3fa7cd582f6f870a622188ff4d9356e4b1b548bf3f797b54589a2413ff2f80` |
| `tc-546-worksheet.txt` | the same PDF, the Payment Worksheet, page layout | same |
| `pub-58.txt` | [Publication 58, Utah Interest and Penalties (Rev. 11/25)](https://files.tax.utah.gov/tax/forms/pubs/pub-58.pdf) (`T/pubs/pub-58.pdf`) | `9a81b8f196a33a35cb93384bd0de75740d541267e4edbe33a0b40c523df3a59b` |
| `ucode-59-10-516.txt` | [Utah Code § 59-10-516, effective 5/5/2021](https://le.utah.gov/xcode/Title59/Chapter10/C59-10-S516_2021050520210505.html) | `e5ee5b88147a8e2a559efa8e39e5b53a4fad45a7476db7ff0891abfb07e640c0` |
| `ucode-59-1-401.txt` | [Utah Code § 59-1-401, effective 1/1/2026](https://le.utah.gov/xcode/Title59/Chapter1/C59-1-S401_2026050620260506.html) | `fdfed96fcc4fef28979984341850714dff5d3bbf6d12a6bfb264bb939a644069` |
| `ucode-59-10-514.txt` | [Utah Code § 59-10-514, effective 5/5/2021](https://le.utah.gov/xcode/Title59/Chapter10/C59-10-S514_2021050520210505.html) | `9faef2fdd1e47a25234b380e7ae28656da5149b4a6a98dd40afb3214e5bcec60` |

## Reproducing them

```bash
T=https://files.tax.utah.gov/tax/forms
C=https://le.utah.gov/xcode/Title59
curl -L -o tc-40inst.pdf "$T/current/tc-40inst.pdf"
curl -L -o tc-546.pdf "$T/current/tc-546.pdf"
curl -L -o pub-58.pdf "$T/pubs/pub-58.pdf"
curl -L -o 59-10-516.html "$C/Chapter10/C59-10-S516_2021050520210505.html"
curl -L -o 59-1-401.html "$C/Chapter1/C59-1-S401_2026050620260506.html"
curl -L -o 59-10-514.html "$C/Chapter10/C59-10-S514_2021050520210505.html"

pdftotext -f 4 -l 5 tc-40inst.pdf tc-40inst-2025-p4-5.txt
pdftotext -f 13 -l 13 tc-40inst.pdf tc-40inst-2025-p13.txt
pdftotext tc-546.pdf tc-546.txt
pdftotext -layout tc-546.pdf - | cut -c77- \
  | awk '/Payment Worksheet/{f=1} f; /subtract line 6 and line 7 from line 5/{if(f)exit}' > tc-546-worksheet.txt
pdftotext pub-58.pdf pub-58.txt
for s in 59-10-516 59-1-401 59-10-514; do
  python3 h2t.py $s.html | sed -n '/^(Effective /,/^Amended by/p' > ucode-$s.txt
done
```

TC-546 is set in two columns, so its worksheet is extracted again with its
layout, from column 77, where the worksheet starts. The PDF prints a control
character after the dash on worksheet line 5, which the extract keeps.

The `C59-1-S401_2026050620260506` address served the version headed
"Effective 1/1/2026", amended by Chapter 299 of the 2026 General Session, on
2026-10-20. Its subsection (5) is the one this package uses; the
reasonable-cause waiver is subsection (14).

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

## There is no worked example

Neither the TC-40 instructions nor Publication 58 works an example of the
extension penalty. The tests compute Publication 58's formula,
`(underpayment) x .24 x (number of days outstanding) / 365`, capped at 12%,
by hand.

## What is not here

- **The 2026 TC-40 instructions.** Not published on 2026-10-20. The 2025
  instructions are the latest, and the rules they describe did not change for
  2026. The 2026 due dates, April 15 and October 15, 2027, follow from
  § 59-10-514(1)(a)(i), which makes the Utah return due when the federal one
  is, and § 59-10-516(1)(b)(i)'s six months, as the 2025 instructions print
  them for 2025.
- **A Utah holiday list.** Neither 2026 date falls on a weekend, and none is
  checked in.
