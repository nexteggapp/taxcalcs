# Where these files came from

Every West Virginia figure in this package is checked against a document the
Tax Division or the Legislature published. These files are text extracted
from them. This records which document, and how, so that anybody can redo it
and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0, in reading order. Code pages extracted with Python 3.12.3
and the script below.

| File | Source | sha256 of the download |
|---|---|---|
| `it140esi-2023.txt` | [IT-140ESI, Estimated Tax Payments Instructions (Sept 2023)](https://tax.wv.gov/Documents/TaxForms/it140esi.pdf), whole document | `bfbc1299c6daf3058f624002dd4395734707ac4f33beef0e7f99d453f4f5960e` |
| `it140-2025-entries.txt` | [2025 West Virginia Personal Income Tax Forms & Instructions](https://tax.wv.gov/Documents/PIT/2025/it140.PersonalIncomeTaxFormsAndInstructions.2025.pdf), PDF page 18, how to enter amounts | `6383bf9748fde18c0469b0eb1b836f4ff6999a2c541bf4e909502a8b3132e0b0` |
| `it140-2025-penalty.txt` | the same PDF, page 24: Penalty for Underpayment of Estimated Tax, Exceptions, Farmers, Waiver, Credit for Estimated Tax | same |
| `it140-2025-waiver.txt` | the same PDF, page 44: Request for Waiver of Estimated Penalty | same |
| `it210-2025.txt` | the same PDF, pages 51 to 55: the IT-210 worksheet, its instructions, and Parts I to IV | same |
| `tsd365.txt` | [TSD-365, How Interest and Additions to Tax are Assessed](https://tax.wv.gov/Documents/TSD/tsd365.pdf) | `8b00e411d32ea7b9776edb68b29d366f26c3c29d3f2a04191f36ec516f1d786b` |
| `wvcode-11-10-17a.txt` | [W. Va. Code § 11-10-17a](https://code.wvlegislature.gov/11-10-17A/) and its bill history | `574dce02a70e003d16b20ee96861667256297d5300e7d381d8fd76552ef3b5bd` |
| `wvcode-11-10-18a.txt` | [W. Va. Code § 11-10-18a](https://code.wvlegislature.gov/11-10-18A/) and its bill history | `75fa720dac2d8648205b95b39bdcb1fb5af627d95c1e71a58e9d9b99e5b586e8` |
| `wvcode-11-21-55.txt` | [W. Va. Code § 11-21-55](https://code.wvlegislature.gov/11-21-55/) and its bill history | `148f3f8852855e1cb3f58ea43609698283faed1cb6c55f7b87a46aa5edca1811` |
| `wvcode-11-21-56.txt` | [W. Va. Code § 11-21-56](https://code.wvlegislature.gov/11-21-56/) and its bill history | `fab7ad960ddb94c7e57b800792ec2ef9d729a7ba9f4edc80cf6fa90324a5c3a7` |
| `wvcode-2-2-1.txt` | [W. Va. Code § 2-2-1, Legal holidays](https://code.wvlegislature.gov/2-2-1/) and its bill history | `58ccddc59ab94dc167cb8e583de2c3e68bbcf2de66509522925dab3def036c33` |

The Code pages are generated HTML and carry a page-load timer, so their
sha256 changes on every download. The extracted text does not.

## Reproducing them

```bash
B=https://tax.wv.gov/Documents
curl -o it140esi.pdf "$B/TaxForms/it140esi.pdf"
curl -o it140.pdf "$B/PIT/2025/it140.PersonalIncomeTaxFormsAndInstructions.2025.pdf"
curl -o tsd365.pdf "$B/TSD/tsd365.pdf"
for s in 11-10-17A 11-10-18A 11-21-55 11-21-56 2-2-1; do
  curl -L -o "code-$s.html" "https://code.wvlegislature.gov/$s/"
done

pdftotext it140esi.pdf it140esi-2023.txt
pdftotext -f 18 -l 18 it140.pdf it140-2025-entries.txt
pdftotext -f 24 -l 24 it140.pdf it140-2025-penalty.txt
pdftotext -f 44 -l 44 it140.pdf it140-2025-waiver.txt
pdftotext -f 51 -l 55 it140.pdf it210-2025.txt
pdftotext tsd365.pdf tsd365.txt

python3 strip.py code-11-10-17A.html §11-10-17a > wvcode-11-10-17a.txt
python3 strip.py code-11-10-18A.html §11-10-18a > wvcode-11-10-18a.txt
python3 strip.py code-11-21-55.html §11-21-55 > wvcode-11-21-55.txt
python3 strip.py code-11-21-56.html §11-21-56 > wvcode-11-21-56.txt
python3 strip.py code-2-2-1.html §2-2-1 > wvcode-2-2-1.txt
```

`strip.py` keeps the section's text, from its heading to the "Previous" link,
and its bill history:

```python
import html, re, sys
src, section = sys.argv[1], sys.argv[2]
t = open(src, encoding="utf-8").read()
t = re.sub(r"(?is)<(script|style).*?</\1>", "", t)
t = re.sub(r"(?i)<br\s*/?>|</p>|</div>|</h\d>", "\n", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [l.strip() for l in t.splitlines() if l.strip()]
start = next(i for i, l in enumerate(lines) if l.startswith(section + "."))
end = next(i for i in range(start, len(lines)) if lines[i].startswith("Previous"))
hist = next(i for i, l in enumerate(lines) if l.startswith("Bill History"))
stop = next(i for i in range(hist, len(lines)) if lines[i].startswith("Signed Bills"))
print("\n".join(lines[start:end] + lines[hist:stop]))
```

## IT-210's chart is the strong check

IT-210 (2025) prints, for each column, the days to April 15, 2026 and the
blended daily rate across 2025's 0.000336 and 2026's 0.000315: 365, 303, 212
and 90 days, and 0.000330, 0.000329, 0.000326 and 0.000315. It is the one
worked example that crosses a real change of rate.
`TestEstimatedRateChangeMatchesTheBlendedChart` reproduces it, including the
303, which needs June 15, 2025, a Sunday, moved to June 16.

## What is not here

- **A 2026 IT-140ES, IT-140ESI or IT-210.** Not published:
  `$B/PIT/2026/it140es.pdf` and `$B/TaxForms/2026/it140es.pdf` return 404.
  The 2025 IT-210's arithmetic is used with the 2026 dates and the 2026 rate
  it prints. The IT-140ES voucher at `$B/PIT/it140es.pdf` is a scanned image
  with no figures, and is not checked in.
- **110CSR21, the personal income tax rule.** The Secretary of State's copy is
  a scanned filing cover, so the rule's text could not be read
  (research/estimated-payments/wv.md, source 7).
- **The rate from January 1, 2027.** Set in December 2026 and not yet
  published. The penalty reports the gap in `Required`.
- **Election days and days the Governor proclaims.** § 2-2-1(a)(13) to (15)
  make them holidays, and no document checked in gives their dates, so the
  calendar leaves them out.
