# Where these files came from

Every Ohio figure in this package is checked against one of these files. The
Department of Taxation's forms are PDFs on dam.assets.ohio.gov; the statutes
and the rule are HTML pages on codes.ohio.gov; HB 96 is an HTML page on the
legislature's site. These files are text extracted from them. One more file is
a labelled copy of quotes from the research file, for documents that could
not be downloaded.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0. HTML was reduced to text with the Python 3.12.3 script
below. `D` is `https://dam.assets.ohio.gov/image/upload` and `C` is
`https://codes.ohio.gov`.

| File | Source | sha256 of the download |
|---|---|---|
| `ites-2026.txt` | [IT 1040ES, 2026 Ohio Estimated Income Tax Payment Worksheet and Instructions](https://dam.assets.ohio.gov/image/upload/v1735926006/tax.ohio.gov/forms/ohio_individual/individual/2026/ites-instructions-fi.pdf), reading order | `0d0766a81a91ce1c41e62e20e8721333ed9e289a354ef1fae522153fd2cbf65c` |
| `ites-2026-layout.txt` | the same PDF, page layout, for the due dates and the tax table | same |
| `itsd2210-2025.txt` | [IT/SD 2210, Interest Penalty on Underpayment of Tax, 2025](https://dam.assets.ohio.gov/image/upload/v1762982566/tax.ohio.gov/forms/ohio_individual/individual/2025/itsd2210-fi.pdf), form and instructions, reading order | `58e7940049cf248709d4829a44ae79117426cac19c3c495e07f0d6accc3f9fde` |
| `it1040-booklet-2025-pages.txt` | [2025 IT 1040 / SD 100 instruction booklet](https://dam.assets.ohio.gov/image/upload/v1767095693/tax.ohio.gov/forms/ohio_individual/individual/2025/it1040-booklet.pdf), pages 5, 10, 17 and 18, one column at a time | `931b21e7484ba96e561cf2346d8a7d2a8005bd990724b5903c9a90acede5f50e` |
| `taxalert-itsd2210-2017.txt` | [Ohio Tax Alert, Updates to Ohio Form IT/SD 2210](https://dam.assets.ohio.gov/image/upload/tax.ohio.gov/OhioTaxAlert/ArchivedAlerts/UPDATESTOOHIOFORM%20ITSD2210.pdf) (tax year 2017) | `b37b62e58dbef1d1f56641ffb627cda4a235ea67120696a710164e17f2e0bdbb` |
| `orc-5747-09.txt` | [ORC 5747.09, Declaration of estimated taxes](https://codes.ohio.gov/ohio-revised-code/section-5747.09), effective September 30, 2025 | `a5ad44b2cb97545c88c7b85994339c1d19f1da6aaf927716eb55ee04381d8199` |
| `orc-5747-09-2018.txt` | [ORC 5747.09, September 28, 2018 version](https://codes.ohio.gov/ohio-revised-code/section-5747.09/9-28-2018) | `1b825d89708021610ac1584fc5ff3329ad82eb4a74a946cfb9b53fe1a5c126c2` |
| `orc-5703-47.txt` | [ORC 5703.47, federal short-term rate; interest rate](https://codes.ohio.gov/ohio-revised-code/section-5703.47) | `bcf4a4476e7d12dc24c260d8c8e64f9c1af53229e510e996c7ae53cbcac03937` |
| `orc-1-14.txt` | [ORC 1.14, legal holidays](https://codes.ohio.gov/ohio-revised-code/section-1.14) | `b8b0a4ed43fe956de92d561e2d060a694f9cf15093a6a5af73548c142f353433` |
| `orc-5747-02.txt` | [ORC 5747.02, tax rates](https://codes.ohio.gov/ohio-revised-code/section-5747.02), effective September 30, 2025 | `960f9336ebcaf1ab4dbe7f75775224e52642d75f03d0f1afa7c134ff1d6c5c3a` |
| `oac-5703-7-04.txt` | [Ohio Adm. Code 5703-7-04, farmers and fishermen](https://codes.ohio.gov/ohio-administrative-code/rule-5703-7-04) | `85ba837786ae92cd69b327d960d810b0409b7fd2e9e5e8c068dc7b4ee3a36793` |
| `hb96-section-801-40.txt` | [Am. Sub. HB 96 (136th General Assembly) as enrolled](https://search-prod.lis.state.oh.us/api/v2/general_assembly_136/legislation/hb96/07_EN/html/), its first lines and Section 801.40 only | `5f6f71fef5e4389a0c010bd4015453f25e65512677a8a4c6cb616717e249bdbe` |
| `research-oh-quotes.txt` | quotes copied from [`research/estimated-payments/oh.md`](../../research/estimated-payments/oh.md) | not fetched |

An HTML page's hash records what was fetched. The page carries navigation that
can change without the section changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
D=https://dam.assets.ohio.gov/image/upload
C=https://codes.ohio.gov
curl -o ites.pdf    "$D/v1735926006/tax.ohio.gov/forms/ohio_individual/individual/2026/ites-instructions-fi.pdf"
curl -o 2210.pdf    "$D/v1762982566/tax.ohio.gov/forms/ohio_individual/individual/2025/itsd2210-fi.pdf"
curl -o booklet.pdf "$D/v1767095693/tax.ohio.gov/forms/ohio_individual/individual/2025/it1040-booklet.pdf"
curl -o alert.pdf   "$D/tax.ohio.gov/OhioTaxAlert/ArchivedAlerts/UPDATESTOOHIOFORM%20ITSD2210.pdf"
curl -o 5747.09.html      "$C/ohio-revised-code/section-5747.09"
curl -o 5747.09-2018.html "$C/ohio-revised-code/section-5747.09/9-28-2018"
curl -o 5703.47.html      "$C/ohio-revised-code/section-5703.47"
curl -o 1.14.html         "$C/ohio-revised-code/section-1.14"
curl -o 5747.02.html      "$C/ohio-revised-code/section-5747.02"
curl -o 5703-7-04.html    "$C/ohio-administrative-code/rule-5703-7-04"
curl -o hb96.html "https://search-prod.lis.state.oh.us/api/v2/general_assembly_136/legislation/hb96/07_EN/html/"

pdftotext ites.pdf ites-2026.txt
pdftotext -layout ites.pdf ites-2026-layout.txt
pdftotext 2210.pdf itsd2210-2025.txt
pdftotext alert.pdf taxalert-itsd2210-2017.txt
# The booklet is set in two columns of 306 points on a 612 by 783 page.
for p in 5 10 17 18; do for x in 0 306; do
  pdftotext -f $p -l $p -x $x -y 0 -W 306 -H 783 booklet.pdf -
done; done > it1040-booklet-2025-pages.txt

cat > html2txt.py <<'PY'
# Prints the text of an HTML page, one block per line, skipping scripts and styles.
import html.parser, re, sys

class Text(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.out, self.skip = [], 0
    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style"):
            self.skip += 1
        if tag in ("p", "div", "br", "li", "h1", "h2", "h3", "tr"):
            self.out.append("\n")
    def handle_endtag(self, tag):
        if tag in ("script", "style"):
            self.skip -= 1
    def handle_data(self, data):
        if not self.skip:
            self.out.append(data)

p = Text()
p.feed(sys.stdin.read())
t = re.sub(r"[ \t]+", " ", "".join(p.out))
print(re.sub(r"\n\s*\n+", "\n\n", t).strip())
PY

# A section from its heading to the list of its versions.
section() { python3 html2txt.py < "$1" | awk -v h="$2" 'index($0,h)==1{f=1} f && /^ *Available Versions/{exit} f' > "$3"; }
section 5747.09.html      "Section 5747.09 |" orc-5747-09.txt
section 5747.09-2018.html "Section 5747.09 |" orc-5747-09-2018.txt
section 5703.47.html      "Section 5703.47 |" orc-5703-47.txt
section 1.14.html         "Section 1.14 |"    orc-1-14.txt
section 5747.02.html      "Section 5747.02 |" orc-5747-02.txt
python3 html2txt.py < 5703-7-04.html |
  awk 'index($0,"Rule 5703-7-04 |")==1{f=1} f{print} f && /^Last updated/{exit}' > oac-5703-7-04.txt
python3 html2txt.py < hb96.html |
  awk 'NR<=4{print} /^801\.40\. The$/{print "Section"; f=1} f && /^$/{exit} f' > hb96-section-801-40.txt
```

The 2025 IT/SD 2210 is extracted in reading order, which keeps its columns'
dates and factors together on one line. The IT 1040ES due dates and tax table
lose their rows in reading order, so they are extracted again with their
layout. The booklet's two columns run together in either mode, so each
column is cropped and extracted on its own.

## The 2025 form's factors are the strong check

Ohio publishes no worked example of the penalty. The 2025 IT/SD 2210 prints
eight line 15 factors, four for individuals and four for pass-through
entities, and page 4 gives the formula they come from: the rate times the days
late, divided by 365.25. `TestEstimatedFactorsReproduceTheTwentyTwentyFiveForm`
works all eight out with the method the 2026 rules use, at 8% for 2025 and 7%
for 2026. The two third columns span the change of rate on January 1, 2026,
which is the nearest published change to the 2026 penalty.

## What could not be downloaded

**tax.ohio.gov.** On 2026-10-20 a plain curl request to each of these pages
was answered with an error page titled "403 Error Page" ("Forbidden Error"),
with HTTP status 404:

- the FAQ, Income - Estimated Income/School District Taxes and the 2210
  Interest Penalty;
- the 2210 Interest Penalty news item of May 7, 2025;
- the Annual Certified Interest Rates page.

No attempt was made to get past the block, and nothing from these pages was
used except the research file's quotes. Those are copied character for
character into `research-oh-quotes.txt`, which says on its first line that it
is a copy. Its verification section read them in a browser on the same day and
marks them VERIFIED. They support the divergence on when the interest stops,
the absence of a first-year safe harbour, and the news item that predates
HB 96.

**The 2026 interest rate journal entry.** The PDF
(`D/tax.ohio.gov/individual/2026_Interest_Rates.pdf`, sha256
`d5532932d0e3cf33924dfe22a712f4b69dcb4de5c3ef497f5e4a7d118f21bc11`) is a
scanned image with no text layer, and no OCR tool was used. The 7% for 2026 is
checked instead against two text documents: the 2025 IT/SD 2210 instructions
and the 2025 IT 1040 booklet. The research file's quote of the journal entry
is in `research-oh-quotes.txt`.

## What is not here

- **The 2026 IT/SD 2210.** Not published on 2026-10-20. The 2025 form's
  arithmetic is used with the 2026 dates, and its line 15 factors are worked
  out by the 2025 formula. The penalty reports that in `Required`.
- **The rate from January 1, 2027.** ORC 5703.47(B) has the commissioner set
  it on October 15, 2026; it was not found published. The penalty reports the
  gap in `Required`.
- **The IT 1040ES revision date.** The worksheet prints none. It prints 2025's
  tax brackets, which ORC 5747.02(A)(3)(c) replaced for 2026, as amended by
  HB 96 effective September 30, 2025. The divergence says so.
- **School district and municipal estimated tax.** Out of scope; no figure
  here comes from the SD 100ES or ORC 718.08.
