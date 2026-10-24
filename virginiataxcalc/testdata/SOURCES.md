# Where these files came from

Every Virginia figure in this package is checked against a document the
Virginia Department of Taxation published, or against the Code of Virginia,
the Virginia Administrative Code or the Acts of Assembly as the General
Assembly publishes them. The Department's forms and the act are PDFs; the
Code, the regulations and the tax bulletins are web pages. These files are
text extracted from them. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; web pages reduced to text with the Python 3.12.3 script
below. `T` is `https://www.tax.virginia.gov`, `L` is
`https://law.lis.virginia.gov`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `760es-2026.txt` | [Form 760ES, 2026 Virginia Estimated Income Tax Payment Vouchers and Instructions for Individuals (Rev. 09/25)](https://www.tax.virginia.gov/sites/default/files/taxforms/individual-income-tax/2026/760es-2026.pdf), reading order, to the first voucher | `fbc7914780244b7f4c5269c1a5ac74e912aa2a4efbe464b1316bc6af533ba7e5` |
| `760es-2026-schedule.txt` | the same PDF, the Payment Schedule, page layout | same |
| `760c-2025.txt` | [Form 760C, Underpayment of Virginia Estimated Tax by Individuals, Estates and Trusts, 2025 (Rev. 04/26)](https://www.tax.virginia.gov/sites/default/files/taxforms/individual-income-tax/2025/760c-2025.pdf), page layout | `a917d491de2c55a3029bac3c625c3de4f226b0a3046bb75fd4adc392fb676f0b` |
| `760c-2025-instructions.txt` | [2025 Instructions for Form 760C (Rev. 12/25)](https://www.tax.virginia.gov/sites/default/files/vatax-pdf/2025-760c-instructions.pdf), reading order | `9e5d101d23aa849692f4b904baa7e950a0f402df131f1d2664eec67359c18368` |
| `760f-2025.txt` | [Form 760F, Underpayment of Virginia Estimated Tax by Farmers, Fishermen, and Merchant Seamen, 2025 (Rev. 12/25)](https://www.tax.virginia.gov/sites/default/files/taxforms/individual-income-tax/2025/760f-2025.pdf), reading order | `513a9c6839ed46154a5994fa68cf029fc9ab42e752fa54f2c3c9fe2156e6285b` |
| `code-58.1-15.txt` | [Code of Virginia § 58.1-15](https://law.lis.virginia.gov/vacode/title58.1/chapter1/section58.1-15/), Rate of interest | `4c33d932825de31f4b83cdf07d8714464c56db4ce1e48ac11a3cf3bbae830e69` |
| `code-58.1-321.txt` | [§ 58.1-321](https://law.lis.virginia.gov/vacode/title58.1/chapter3/section58.1-321/), Exemptions and exclusions | `b639dec298b0d85257bbabd568f1fc94045a26aa15d0e6bb265f0e63fe8ea7f6` |
| `code-58.1-490.txt` | [§ 58.1-490](https://law.lis.virginia.gov/vacode/title58.1/chapter3/section58.1-490/), Declarations of estimated tax | `4379bb6482962c623e4bdf86771b261c198d2ac1944b0bf6c5648ad9bb3f99e0` |
| `code-58.1-491.txt` | [§ 58.1-491](https://law.lis.virginia.gov/vacode/title58.1/chapter3/section58.1-491/), Payments of estimated tax | `d6d5642f416f117460ea22d1d94c809f2c8814be908f87aed31573479cab54a0` |
| `code-58.1-492.txt` | [§ 58.1-492](https://law.lis.virginia.gov/vacode/title58.1/chapter3/section58.1-492/), Failure by individual, trust or estate to pay estimated tax | `ab0fd46ef45f8eba7aa68906967eafa90a3869340af484a9206c4265b272f2cf` |
| `code-2.2-3300.txt` | [§ 2.2-3300](https://law.lis.virginia.gov/vacode/title2.2/chapter33/section2.2-3300/), Legal holidays | `6fa47e2a6d27ea2d1125b01cbce4fe58b2294ae5b62b834ad4d6c4d010482d35` |
| `23vac10-112-20.txt` | [23VAC10-112-20](https://law.lis.virginia.gov/admincode/title23/agency10/chapter112/section20/), Declarations of estimated tax; requirement of declaration | `2ddf55e730e6aac622df8bc7caf64d759d3f3797b9c02996718a1e49d91d1db7` |
| `23vac10-112-42.txt` | [23VAC10-112-42](https://law.lis.virginia.gov/admincode/title23/agency10/chapter112/section42/), period of underpayment, with two examples | `fcb1218ead9adbda2e86e350f106d535149f2d7746072c3d9238b07e56790d9c` |
| `23vac10-112-43.txt` | [23VAC10-112-43](https://law.lis.virginia.gov/admincode/title23/agency10/chapter112/section43/), exception, with five examples | `6f8fa1af026ac9fb8785cfabecd5a93bdf7ea82f5d57ee94c64978afc5607bd6` |
| `tb-25-7.txt` | [Tax Bulletin 25-7](https://www.tax.virginia.gov/laws-rules-decisions/tax-bulletins/25-7), interest rates for the first quarter of 2026 | `5c7ef841d891bce6c91077209a24282d11c4fd9d02e91d7c7df81835f6e6ad8b` |
| `tb-26-2.txt` | [Tax Bulletin 26-2](https://www.tax.virginia.gov/laws-rules-decisions/tax-bulletins/26-2), the second quarter | `3369a529bc5efef4ab67e6ce0f5d1533e541ddef24662b120be807120f59c841` |
| `tb-26-3.txt` | [Tax Bulletin 26-3](https://www.tax.virginia.gov/laws-rules-decisions/tax-bulletins/26-3), the third quarter | `9792ef9b8b4e5e66d05562b02b381888dbe70ddd12f4d3d63b9ce3fb3bfd0b45` |
| `tb-26-4.txt` | [Tax Bulletin 26-4](https://www.tax.virginia.gov/laws-rules-decisions/tax-bulletins/26-4), the fourth quarter, with the table of recent rates | `5e17b80ed69e1146c447f73ebbd1f64b14afc647a6bb5fe36a43873f6d24fe71` |
| `acts-2026-c7.txt` | [2026 Acts of Assembly, Chapter 7 (H 29)](https://budget.lis.virginia.gov/get/budget/5228/HB29/): its heading and the fifth and sixth enactments | `9e1665f8416479d13fd3308ca3470294ba09482d6c5ce28e053dd4f4bb06d5db` |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
T=https://www.tax.virginia.gov
L=https://law.lis.virginia.gov
curl -o 760es-2026.pdf "$T/sites/default/files/taxforms/individual-income-tax/2026/760es-2026.pdf"
curl -o 760c-2025.pdf "$T/sites/default/files/taxforms/individual-income-tax/2025/760c-2025.pdf"
curl -o 2025-760c-instructions.pdf "$T/sites/default/files/vatax-pdf/2025-760c-instructions.pdf"
curl -o 760f-2025.pdf "$T/sites/default/files/taxforms/individual-income-tax/2025/760f-2025.pdf"
curl -o hb29.pdf https://budget.lis.virginia.gov/get/budget/5228/HB29/
for s in 58.1-490 58.1-491 58.1-492 58.1-321; do
  curl -o code-$s.html "$L/vacode/title58.1/chapter3/section$s/"
done
curl -o code-58.1-15.html "$L/vacode/title58.1/chapter1/section58.1-15/"
curl -o code-2.2-3300.html "$L/vacode/title2.2/chapter33/section2.2-3300/"
for r in 20 42 43; do
  curl -o vac-23-10-112-$r.html "$L/admincode/title23/agency10/chapter112/section$r/"
done
for b in 25-7 26-2 26-3 26-4; do
  curl -o tb-$b.html "$T/laws-rules-decisions/tax-bulletins/$b"
done

cat > html2text.py <<'PY'
# Reduces a web page to one line of text per block element, and one line per
# table row. Whitespace in the HTML source is collapsed first.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8", errors="replace").read()
t = re.sub(r"\s+", " ", t)
t = re.sub(r"(?is)<script.*?</script>|<style.*?</style>|<!--.*?-->", "", t)
t = re.sub(r"(?is)<tr.*?</tr>", lambda m: re.sub(r"(?i)<(br|/p|/li|/div)[^>]*>", " ", m.group(0)), t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY

pdftotext 760es-2026.pdf es.txt
awk '/^2026 FORM 760ES - Voucher 1$/{exit} 1' es.txt > 760es-2026.txt
pdftotext -layout 760es-2026.pdf es.L.txt
awk '/^ *PAYMENT SCHEDULE$/{f=1} f; /after Sept\. 1st and before Dec\. 31st/{if(f)exit}' es.L.txt > 760es-2026-schedule.txt
pdftotext -layout 760c-2025.pdf 760c-2025.txt
pdftotext 2025-760c-instructions.pdf 760c-2025-instructions.txt
pdftotext 760f-2025.pdf 760f-2025.txt
for s in 58.1-15 58.1-321 58.1-490 58.1-491 58.1-492 2.2-3300; do
  python3 html2text.py code-$s.html | awk -v s="$s" 'index($0, "§ " s ". ") == 1 && /\.$/{f=1} f; /^Code 1950/{if(f)exit}' > code-$s.txt
done
for r in 20 42 43; do
  python3 html2text.py vac-23-10-112-$r.html | awk -v r="23VAC10-112-$r. " 'index($0, r) == 1{n++} n==2; /^Statutory Authority/{if(n==2)exit}' > 23vac10-112-$r.txt
done
for b in 25-7 26-2 26-3 26-4; do
  python3 html2text.py tb-$b.html | awk -v b="TAX BULLETIN $b" '$0 == b{f=1} /^Additional Information:/{exit} f' > tb-$b.txt
done
pdftotext hb29.pdf hb29.txt
{ awk '/^Be it enacted by the General Assembly of Virginia:$/{print; exit} 1' hb29.txt
  awk '/^5\. That § 58\.1-492 of the Code of Virginia is amended and reenacted as follows:$/{f=1} f; /^6\. That the provisions of the fifth enactment/{if(f)exit}' hb29.txt
} > acts-2026-c7.txt
```

The 760ES and the 760C instructions are set in two columns, so they are
extracted in reading order. The 760ES payment schedule loses its rows in
reading order, so it is extracted again with its layout. Form 760C itself is a
grid of lines and columns and reads best with its layout. The bounds are the
documents' own headings rather than line numbers. The act is the whole
biennial budget, 355 pages; only its heading and the two enactments that
amend § 58.1-492 are kept. Its text shows the struck "$150" and the inserted
"$1,000" run together, as "$150$1,000".

## The regulations' examples are the strong check

23VAC10-112-42 and -43 work examples with stated answers. Example 2 of -42
gives the order a September overpayment pays earlier shortfalls in ($925,
$325, then $625 to January), which is not the federal order. Example 1 of -42
gives the $750 underpayments and the day counts. Examples 4 and 5 of -43 give
the amounts Exceptions 3 and 4 need. The Form 760C instructions give two more:
a late payment split between two columns, and one underpayment paid in two
parts. Each is a test.

Example 1 of -42 writes its charges as 12% × days ÷ 365, but its answers for
the second to fourth installments ($78.75, $56.25, $26.25) are 12% for 10.5,
7.5 and 3.5 months, not for 320, 228 and 106 days ($78.90, $56.22, $26.14).
Its rate is 1988's, so the tests check its underpayments and days and not its
charges.

## What is not here

- **The 2026 Form 760C.** Not published: the 2026 path returned 404 on
  2026-10-20. The 2025 form's arithmetic is used with the 2026 dates, and with
  the 2026 law's $1,000 in place of its $150.
- **A daily rate for 8%.** Form 760C prints .00025 for 9% and nothing for
  8%. Days at 8% are charged at 8% × days ÷ 365, and the result says so in
  `Required`.
- **The rate from January 1, 2027.** Not published; the bulletin for the first
  quarter of 2027 did not exist on 2026-10-20. The penalty reports the gap.
- **The Penalties and Interest page.** It states only the rule, federal rate
  plus 2%, which § 58.1-15 and the bulletins carry.
