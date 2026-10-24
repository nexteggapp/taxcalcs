# Where these files came from

Every Illinois figure in this package is checked against a document the
Illinois Department of Revenue or the General Assembly published. These files
are text extracted from them. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. `B` is
`https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `il-1040-es-2026.txt` | [IL-1040-ES, Estimated Income Tax Payments for Individuals 2026](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-1040-es.pdf) (`B/il-1040-es.pdf`), R-09/26, whole document, reading order | `da843545969048187e3a4341af10c198fae4a01431c63f03457e1da4829bb6e9` |
| `il-2210-2025.txt` | [2025 IL-2210, Computation of Penalties for Individuals](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-2210.pdf) (`B/il-2210.pdf`), R-12/25, whole form, page layout | `4bb1ba097544532e41657fb1c769654acadf77a7a1a8c57eb67d68510e678fe7` |
| `il-2210-2025-instructions.txt` | [2025 Form IL-2210 Instructions](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-2210-instr.pdf) (`B/il-2210-instr.pdf`), R-12/25, whole document, reading order | `eb4efbf9588a5b278714005f5992780d15ce2d7adf1126f58f5bf568bc0284ca` |
| `il-2210-2025-example.txt` | the same PDF, the Example on page 5, page layout | same |
| `il-1040-2025-payments-and-penalty.txt` | [2025 Form IL-1040](https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual/il-1040.pdf) (`B/il-1040.pdf`), R-12/25, Step 8 to the end of Step 10, page layout | `7af662f99b31f6aa0d00df5ae93c2a50c4c15e01a0c1dcf5f722fc0bde0799cd` |
| `pub-103-2025.txt` | [Publication 103, Penalties and Interest for Illinois Taxes](https://tax.illinois.gov/content/dam/soi/en/web/tax/research/publications/pubs/documents/pub-103.pdf), R-09/25: Interest, the estimated-tax penalty, and Requesting Penalty Abatement, reading order | `e43568685b28cf3e51fb494220eadca7fb5cffaa0bcb562045b13e3f91187d61` |
| `idor-interest-rates.txt` | [IDOR, Interest Rates](https://tax.illinois.gov/individuals/interestrate.html), the explanation and the current row | `2d7830a551db5d75116e1a136a01a031acd09620447551d7173389db8590ac6c` (the HTML) |
| `adm-100-5000.txt` | [86 Ill. Adm. Code 100.5000](https://ftp.ilga.gov/JCAR/AdminCode/086/086001000O50000R.html), whole section | `a5d06d724e3180c07ae49ac875aebba25f6d4ef4eb4cba547507ec1901ec9463` (the HTML) |
| `adm-100-8000.txt` | [86 Ill. Adm. Code 100.8000](https://ftp.ilga.gov/JCAR/AdminCode/086/086001000W80000R.html), whole section | `82c9cc67fda70459f274e640e3bcd9f461491b4afafc244cc7f355d65b001829` (the HTML) |
| `adm-100-8010.txt` | [86 Ill. Adm. Code 100.8010](https://ftp.ilga.gov/JCAR/AdminCode/086/086001000W80100R.html), whole section | `1430a8e21b6c7b45ab296b7eb2b1cbc58db35fafa33c5b6f9c79d1389b8ab0b9` (the HTML) |
| `adm-700-220.txt` | [86 Ill. Adm. Code 700.220](https://ftp.ilga.gov/JCAR/AdminCode/086/086007000B02200R.html), whole section | `d89210fd0f2d08d0b7b03e82ee666b18f9e2a4087775cdca8fd972d2da9d0546` (the HTML) |
| `adm-700-400.txt` | [86 Ill. Adm. Code 700.400](https://ftp.ilga.gov/JCAR/AdminCode/086/086007000D04000R.html), whole section | `1b9f2beb0adee45b446a5a8b553d096d5ed67f17c232e96002c05a903c01afaa` (the HTML) |
| `research-il-statute-quotes.txt` | **Not fetched.** Lines copied unchanged from [`research/estimated-payments/il.md`](../../research/estimated-payments/il.md); see below | — |

An HTML page's hash records what was fetched. A page can carry navigation that
changes without its content changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

ftp.ilga.gov serves its certificate without the intermediate, so curl cannot
verify it from the system store alone. The intermediate is fetched from the
URL the certificate itself names (its Authority Information Access field) and
added to the store; verification is not turned off.

```bash
B=https://tax.illinois.gov/content/dam/soi/en/web/tax/forms/incometax/documents/currentyear/individual
curl -o il-1040-es.pdf    "$B/il-1040-es.pdf"
curl -o il-2210.pdf       "$B/il-2210.pdf"
curl -o il-2210-instr.pdf "$B/il-2210-instr.pdf"
curl -o il-1040.pdf       "$B/il-1040.pdf"
curl -o pub-103.pdf https://tax.illinois.gov/content/dam/soi/en/web/tax/research/publications/pubs/documents/pub-103.pdf
curl -o rates.html  https://tax.illinois.gov/individuals/interestrate.html

curl -o r40.crt http://crt.sectigo.com/SectigoPublicServerAuthenticationCAOVR40.crt
openssl x509 -inform DER -in r40.crt -out r40.pem
cat /etc/ssl/certs/ca-certificates.crt r40.pem > chain.pem
J=https://ftp.ilga.gov/JCAR/AdminCode/086
for a in 086001000O50000R 086001000W80000R 086001000W80100R 086007000B02200R 086007000D04000R; do
  curl --cacert chain.pem -o $a.html "$J/$a.html"
done

pdftotext il-1040-es.pdf il-1040-es-2026.txt
pdftotext -layout il-2210.pdf il-2210-2025.txt
pdftotext il-2210-instr.pdf il-2210-2025-instructions.txt
pdftotext -layout il-2210-instr.pdf il-2210-instr.L.txt
awk '/^\f? +Example$/{f=1} f; /Page 5 of 5/{if(f)exit}' il-2210-instr.L.txt > il-2210-2025-example.txt
pdftotext -layout il-1040.pdf il-1040.L.txt
awk '/Step 8: Payments and Refundable Credit/{f=1} /Step 11: Refund or Amount you owe/{if(f)exit} f' il-1040.L.txt \
  > il-1040-2025-payments-and-penalty.txt
pdftotext pub-103.pdf pub-103.txt
awk '/^\f?Interest$/{f=1} /^\f?Interest paid by the State of Illinois$/{f=0}
     /^\f?Late-payment penalty for underpayment of estimated or quarter-monthly$/{f=1} /^\f?Bad check penalty$/{f=0}
     /^\f?Requesting Penalty Abatement$/{f=1} /^\f?Contact Information$/{f=0} f' pub-103.txt > pub-103-2025.txt

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding=sys.argv[2] if len(sys.argv) > 2 else "utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0​]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py rates.html \
  | awk '/^On January 1, 1994, a uniform interest rate/{f=1} /^01\/01\/2024 - 12\/31\/2024/{if(f)exit} f' \
  > idor-interest-rates.txt
python3 html2text.py 086001000O50000R.html latin-1 > adm-100-5000.txt
python3 html2text.py 086001000W80000R.html latin-1 > adm-100-8000.txt
python3 html2text.py 086001000W80100R.html latin-1 > adm-100-8010.txt
python3 html2text.py 086007000B02200R.html latin-1 > adm-700-220.txt
python3 html2text.py 086007000D04000R.html latin-1 > adm-700-400.txt
```

The IL-1040-ES and the IL-2210 instructions are set in two columns, so they
are extracted in reading order. The IL-2210 form keeps its layout so that its
percentage rows stay on one line, and the worked example is extracted again
with its layout because its table loses its rows in reading order. The
Administrative Code pages are served as ISO-8859-1.

## The statutes were refused

www.ilga.gov answered `HTTP 403`, an "Access Denied" page, for every section
of the Illinois Compiled Statutes requested by curl on 2026-10-20: 35 ILCS
5/803, 5/804 and 5/806, and 35 ILCS 735/3-2, 3-3 and 3-8, at
`https://www.ilga.gov/Documents/legislation/ilcs/documents/<DocName>.htm`.
The research file notes the site blocks automated readers. Nothing here works
around that.

The statute side of each figure and divergence therefore rests on
`research/estimated-payments/il.md`, whose verification read the statutes.
`research-il-statute-quotes.txt` holds the lines of that file that quote them,
copied unchanged with:

```bash
R=research/estimated-payments/il.md
grep -E '\[(ILCS-803|ILCS-804|ILCS-806|UPIA-3-3|UPIA-3-2|UPIA-3-8)\]' $R \
  | grep -v '^- \[\(ILCS\|UPIA\)-[0-9-]*\] [0-9]* ILCS'
grep -E 'P\.A\. 101-355 raised|Subsection \(b-20\)\(1\)|subsection \(b-25\) applies to returns|\*\*Prior-year return conditions\.\*\* Resolved|\*\*Withholding election\.\*\* Resolved|\*\*Revision code\.\*\* Partly|^- No Public Act from|^- SB2041' $R
```

after a three-line header saying where they came from. Every figure the rules
use is also in a department document above; the quotes check that the Act
agrees.

## The worked examples are the strong check

The IL-2210 instructions work one example to the cent: $1,800 of tax, $2,350
last year, five payments, and $121.20 of penalty across seven applications of
payments to quarters. `TestEstimatedPenaltyMatchesIL2210Example` reproduces
every line on 2026 dates. The second due date moves from June 16, 2025 (June
15 was a Sunday) to June 15, 2026, which turns 84 days into 85, still 10%.

86 Ill. Adm. Code 100.8010(d)(2) Examples 1 to 3 work the annualised method:
$13,648 a year, annualised installments of $1,278, $1,660 and $3,414, and
$7,296 left for January. `TestEstimatedAnnualisedMatchesTheRulesExamples`
reproduces them.

## What is not here

- **The 2026 IL-2210.** Not published; `B/il-2210.pdf` is still the 2025
  form. Its arithmetic is used with the 2026 figures from the IL-1040-ES.
- **The statutes.** Refused, as above.
- **The interest rate from January 1, 2027.** Not published. When interest is
  asked for, the penalty reports the gap in `Required`.
- **An Illinois holiday list.** None is checked in. Every 2026 due date the
  IL-1040-ES prints is a weekday, so only weekends are applied, and the tests
  check the computed dates against the printed ones.
