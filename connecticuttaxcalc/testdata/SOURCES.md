# Where these files came from

Every Connecticut figure in this package is checked against a document the
Department of Revenue Services (DRS) published, or against the General
Statutes as the Connecticut General Assembly publishes them. The DRS documents
are PDFs and the statutes are web pages; these files are text extracted from
them. This records which document, and how, so that anybody can redo it and
diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; web pages reduced to text with the Python 3.12.3 script
below.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `ct-1040es-2026.txt` | [Form CT-1040ES, 2026 Estimated Connecticut Income Tax Payment Coupon for Individuals (Rev. 01/26)](https://portal.ct.gov/-/media/drs/forms/2025/income/ct1040es-flat0126.pdf), reading order, from the start to the worksheet's end and the Line 14 and Line 16a instructions; the tax tables are left out | `9acc155236a24c9bce3ab62d6604036c16d60bb425c37778814d770f34765068` |
| `ct-2210-2025.txt` | [Form CT-2210, Underpayment of Estimated Income Tax by Individuals, Trusts, and Estates (Rev. 12/25)](https://portal.ct.gov/-/media/drs/forms/2025/income/ct-2210_1225.pdf), form and instructions, reading order, without Worksheet 1 (pages 5 and 6, nonresidents' annualised tax) | `9559ebc66de7385d50f7597909e505b13786944623c5b4d950d67b5ca9fe56b8` |
| `cgs-chap229.txt` | [Conn. Gen. Stat. chapter 229, revised to January 1, 2025](https://www.cga.ct.gov/current/pub/chap_229.htm): § 12-701(a)(12) and § 12-722 with its history | `9bda4ecdbbe3937466237f934178f2d1d478b74f586d49cafdca262844467d8a` (the HTML) |
| `cgs-chap229-2026-supplement.txt` | [2026 Supplement to chapter 229](https://www.cga.ct.gov/2026/sup/chap_229.htm): its table of contents and § 12-701(a)(12) | `2faa8eebcad4e951d01bfd563941ba74e4f8d7d2b0d29a84a631cf19f8752511` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

www.cga.ct.gov serves an incomplete certificate chain: it omits the GoDaddy
Secure Certificate Authority - G2 intermediate. The intermediate is fetched
from the URL in the site certificate's own Authority Information Access field
and added to the system roots. Certificate verification stays on.

```bash
curl -o ct1040es.pdf https://portal.ct.gov/-/media/drs/forms/2025/income/ct1040es-flat0126.pdf
curl -o ct2210.pdf   https://portal.ct.gov/-/media/drs/forms/2025/income/ct-2210_1225.pdf

# The intermediate; its sha256 was 973a41276ffd01e027a2aad49e34c37846d3e976ff6a620b6712e33832041aa6.
curl -L -o gdig2.crt http://certificates.godaddy.com/repository/gdig2.crt
openssl x509 -inform DER -in gdig2.crt -out gdig2.pem
cat /etc/ssl/certs/ca-certificates.crt gdig2.pem > bundle.pem
curl --cacert bundle.pem -o chap229.htm https://www.cga.ct.gov/current/pub/chap_229.htm
curl --cacert bundle.pem -o sup229.htm  https://www.cga.ct.gov/2026/sup/chap_229.htm

pdftotext ct1040es.pdf es.txt
{ awk '/^Tax Calculation Schedule$/{exit} 1' es.txt
  awk '/^Line 14: If your 2025 Connecticut income tax return covered a 12/{f=1} /^Payment Options$/{if(f)exit} f' es.txt
} > ct-1040es-2026.txt

pdftotext ct2210.pdf 2210.txt
awk '/^\f?Worksheet 1$/{skip=1} !skip; /^Page 6 of 10$/{skip=0}' 2210.txt > ct-2210-2025.txt

cat > html2text.py <<'PY'
# Reduces a cga.ct.gov chapter page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8", errors="replace").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py chap229.htm > chap229.txt
python3 html2text.py sup229.htm  > sup229.txt
{ grep -m1 '^(12) “Required annual payment” means' chap229.txt
  awk '/^Sec\. 12-722\. Underpayment and payment of estimated tax\. Interest\. Credit\. Payment schedule for farmers and fishermen\. \(a\)/{f=1} f; /^History: June Sp\. Sess\. P\.A\. 91-3, S\. 73/{if(f)exit}' chap229.txt
} > cgs-chap229.txt
{ awk '/^Sec\. 12-701\. Definitions\. Regulations\. \(a\)/{exit} 1' sup229.txt
  grep -m1 '^(12) “Required annual payment” means' sup229.txt
} > cgs-chap229-2026-supplement.txt
```

Both DRS documents are set in two columns, so they are extracted in reading
order. The bounds are the documents' own headings rather than line numbers. The
CT-1040ES extract runs to the Tax Calculation Schedule heading, which follows
the worksheet, and then picks up the Line 14 and Line 16a instructions on page
8; the tax tables between them are not needed. Reading order puts some
paragraphs out of place, notably in CT-2210 Part 1, where the text of Boxes F
and G is split; the tests quote the instructions on page 7 instead.

## No published worked example

CT-2210's examples (Anita, Bill, Jim, Sara, David, Eric, Kim and Tim) say which
test applies to whom. None carries figures, so there is no published answer to
reproduce. The tests are hand-worked cases, each worked in its comment both the
way CT-2210 Schedule B computes it and installment by installment as
§ 12-722(b) reads.

## What is not here

- **The 2026 CT-2210.** Not published. The 2025 form and instructions are used
  for the line arithmetic, the Schedule A periods and percentages, and the
  Schedule B months, with 2026's dates from CT-1040ES. The 1% a month is
  § 12-722(a)'s, not a yearly figure.
- **§§ 12-699 and 12-699a**, the Pass-Through Entity Tax and its credit, in
  chapter 228z. Not checked in. The statute side of the two PE Tax Credit
  divergences cites them as `research/estimated-payments/ct.md`'s verification
  read them, and rests otherwise on § 12-722(j)(1) and (k), which are here.
- **A Connecticut holiday list.** No DRS document listing the holidays that
  move a due date is checked in. Every 2026 due date CT-1040ES prints is a
  weekday, so only weekends are applied, and the tests check the computed
  dates against the printed ones. § 12-722(c) itself does not move a date;
  the weekend and holiday rule is CT-1040ES's.
- **Informational Publication 2018(11) and Worksheet CT-1040 AES**, the
  department's guide to the annualised method. Not needed: CT-2210 Schedule A
  carries the same periods and percentages.
