# Where these files came from

Every Mississippi figure in this package is checked against a document the
Mississippi Department of Revenue, the Secretary of State or the Legislature
published. These files are text extracted from those documents. This records
which document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0. The bill was reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`, after converting it from
ISO-8859-1 with iconv.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `form-80-320-2025.txt` | [Form 80-320, Individual Income Tax Interest and Penalty Worksheet, instructions and example (Rev. 09/25)](https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80320258%201.pdf), whole form, page layout | `9c2c621d0a9f8927f0fd71d954a12998f6420af1e1b0f49e3e308d2bdf9ddc78` |
| `form-80-106-2025.txt` | [Form 80-106, Individual / Fiduciary Income Tax Voucher (Rev. 09/25)](https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80106258%201.pdf), Who Must Make Estimated Tax Payments, page layout | `95addebf51fb5ad30628e25a4473ee478ebbbc62f839c556970c99fa3dbce7bd` |
| `form-80-100-2025-estimated.txt` | [Form 80-100, 2025 Resident Individual Income Tax Instructions](https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80100251%202.pdf), the passages on estimated tax, reading order | `f0d2eb70d26f0934dba9d7dbceb21ee519d038061210f703c084cfcc85f85824` |
| `rule-35-III-11-21-100.txt` | [35 Miss. Admin. Code Pt. III](https://www.sos.ms.gov/adminsearch/ACCode/00000158c.pdf) (PDF modified May 26, 2026), Subpart 11, Chapter 21, Rule 100, page layout | `e17952fdc2bcf0d7430ae0e13beee452609610f45af63027fb280b884197f1b8` |
| `hb799-2014-27-7-327.txt` | [2014 Regular Session House Bill 799, as sent to the Governor](https://billstatus.ls.state.ms.us/documents/2014/html/HB/0700-0799/HB0799SG.htm), Section 7, amending § 27-7-327 | `bdd537617eb59c377b4eadf522bf479261a400e42bcc8a20c633b432fa2de793` (the HTML) |

## Reproducing them

```bash
curl -o 80-320.pdf "https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80320258%201.pdf"
curl -o 80-106.pdf "https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80106258%201.pdf"
curl -o 80-100.pdf "https://www.dor.ms.gov/sites/default/files/tax-forms/individual/80100251%202.pdf"
curl -o rule.pdf   "https://www.sos.ms.gov/adminsearch/ACCode/00000158c.pdf"
curl -o gsrsaovsslca2018.crt "http://secure.globalsign.com/cacert/gsrsaovsslca2018.crt"
openssl x509 -inform DER -in gsrsaovsslca2018.crt -out gsrsaovsslca2018.pem
openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt gsrsaovsslca2018.pem   # OK
cat /etc/ssl/certs/ca-certificates.crt gsrsaovsslca2018.pem > bundle.pem
curl --cacert bundle.pem -o hb799.htm "https://billstatus.ls.state.ms.us/documents/2014/html/HB/0700-0799/HB0799SG.htm"

pdftotext -layout 80-320.pdf form-80-320-2025.txt
pdftotext -layout 80-106.pdf 80-106.L.txt
awk '/Who Must Make Estimated Tax Payments/{f=1} /Return Payments/{if(f)exit} f' 80-106.L.txt > form-80-106-2025.txt
pdftotext -layout rule.pdf rule.L.txt
awk '/^100   INDIVIDUAL ESTIMATED TAX PAYMENTS\./{f=1} /^101   CORPORATE ESTIMATED TAX PAYMENTS\./{if(f)exit} f' rule.L.txt > rule-35-III-11-21-100.txt

pdftotext 80-100.pdf 80-100.txt
cat > passages.py <<'PY'
# Prints each passage of a text file that runs from a line equal to a start
# heading up to, not including, the next line equal to its end heading.
# usage: passages.py FILE START1 END1 [START2 END2 ...]
import sys
lines = open(sys.argv[1], encoding="utf-8").read().split("\n")
pairs = sys.argv[2:]
for start, end in zip(pairs[::2], pairs[1::2]):
    i = next(n for n, l in enumerate(lines) if l.strip() == start)
    j = next(n for n in range(i + 1, len(lines)) if lines[n].strip() == end)
    print("\n".join(lines[i:j]).rstrip() + "\n")
PY
python3 passages.py 80-100.txt \
  "Generally, you must file a Declaration of Estimated Tax, Form" "Failure to File: The penalty for failure to file a return is" \
  "Underestimating the required amount of tax or failure to file" "Your return may be audited by the IRS or the Department of" \
  "Exceptions: You will not have to pay the underestimated" "TAX RATES" \
  "• Filing requirements met after the payment due date." "File an amended return to:" \
  "An individual taxpayer is subject to making" "Contributions to the Mississippi Wildlife" \
  "Farmers or Fishermen Exception" "If you are not due a refund but wish to contribute," \
  > form-80-100-2025-estimated.txt

iconv -f latin1 -t utf-8 hb799.htm > hb799.u.htm
python3 html2text.py hb799.u.htm > hb799.txt   # the script in minnesotataxcalc/testdata/SOURCES.md
awk '/^27-7-327, Mississippi Code of 1972, is amended as follows:/{f=1} /^SECTION 8\./{if(f)exit} f' hb799.txt > hb799-2014-27-7-327.txt
```

The booklet is set in columns, so it is extracted in reading order and each
passage is bounded by its own first and following lines.

**billstatus.ls.state.ms.us serves an incomplete certificate chain.** It sends
its own certificate and not the intermediate that issued it, so plain curl
refuses it ("unable to get local issuer certificate"). The bill is fetched
with the chain completed rather than with verification turned off:

- The server's certificate names its issuer, GlobalSign RSA OV SSL CA 2018,
  and that issuer's certificate at
  `http://secure.globalsign.com/cacert/gsrsaovsslca2018.crt`, in its
  Authority Information Access field.
- That file is GlobalSign's published intermediate. Its sha256 is
  `b676ffa3179e8812093a1b5eafee876ae7a6aaf231078dad1bfb21cd2893764a`, and it
  is valid from 2018-11-21 to 2028-11-21.
- It is served over plain HTTP, so it is trusted only because `openssl verify`
  checks its signature against GlobalSign Root CA - R3 in the system's CA
  bundle (Ubuntu 24.04 `ca-certificates` 20260601~24.04.1).
- curl is then given the system bundle plus the intermediate with `--cacert`,
  and verifies the server normally. curl 8.5.0 with OpenSSL 3.0.13, on
  2026-10-20.

The HTML fetched this way has the same sha256 as the copy first fetched
with `-k`, and the extract rebuilt from it is byte-identical to the one
checked in.

## Form 80-320's example is the strong check

Form 80-320 works one example: $6,520 of liability, $4,510 last year, $1,500,
$1,000, $500 and nothing paid by the four dates, and $30.30 of interest.
Inputs and answer both come from the department.
`TestEstimatedInterestMatchesTheFormExample` reproduces it with the 2026
dates, which count the same 2, 3, 4 and 3 months.

## What is not here

- **The 2026 Form 80-320 and Form 80-106.** Not posted. The Rev. 09/25 forms
  are used with the 2026 dates.
- **Miss. Code §§ 27-7-319 and 27-7-329, and the current text of
  § 27-7-327.** The official code is on the LexisNexis portal, which does not
  serve a script. § 27-7-327 is checked in as 2014 H.B. 799 amended it;
  `research/estimated-payments/ms.md` found no later amendment.
- **A rule for due dates on a weekend or holiday, a January 31 rule, a waiver,
  or a part-month rule.** None was found in any Mississippi source; see
  `research/estimated-payments/ms.md`.
