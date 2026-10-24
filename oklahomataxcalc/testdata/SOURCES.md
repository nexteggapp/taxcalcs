# Where these files came from

Every Oklahoma figure in this package is checked against a document the
Oklahoma Tax Commission (OTC) published, or against the Oklahoma Statutes as
the Oklahoma Legislature publishes them. Those documents are PDFs and one web
page; these files are text extracted from them. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; the web page reduced to text with the Python 3.12.3 script
below. `F` is `https://oklahoma.gov/content/dam/ok/en/tax/documents/forms`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `ow-8-es-2026.txt` | [Form OW-8-ES, Oklahoma Individual Estimated Tax, tax year 2026 (revised 6-2025)](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-ES.pdf) (`F/individuals/current/OW-8-ES.pdf`), whole document, reading order | `27fbbbfca6443a238732c459f95c47b73e171ee094622a700ac9e2a60331ae1a` |
| `ow-8-es-sup-2026.txt` | [Form OW-8-ES-SUP, Annualized Estimated Tax Worksheet, 2026](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-ES-SUP.pdf), whole document, page layout | `e73a727a144ad27174923dc5a8a712dc150f050c667938c864e5e9166359c461` |
| `ow-8-p-2025.txt` | [Form OW-8-P, Underpayment of Estimated Tax Worksheet, 2025](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/other/OW-8-P.pdf) (`F/other/OW-8-P.pdf`), whole document, reading order | `6fdb3856fdbae44839b4d69c7a7fafca2f3954074c0497882049568c815ec8d5` |
| `ow-8-p-sup-i-2025.txt` | [Form OW-8-P-SUP-I, Annualized Income Installment Method for Individuals, 2025](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/OW-8-P-SUP-I.pdf), whole document, page layout | `5fbd16030f13ee260f6a93081b83dc8199cf041dfb31f8fc25f41b7bae38bafd` |
| `511-pkt-2025-estimated.txt` | [2025 Oklahoma Resident Individual Income Tax Forms and Instructions](https://oklahoma.gov/content/dam/ok/en/tax/documents/forms/individuals/current/511-Pkt.pdf) (`F/individuals/current/511-Pkt.pdf`): Due Date, Estimated Income Tax, and the line 22 and line 40 instructions, reading order | `c90cf30381cca6262ec94050acde8db1ee58885360f0af4ec21e045d1cc5a572` |
| `os68-2385.txt` | [Oklahoma Statutes Title 68, complete title](https://www.oklegislature.gov/OK_Statutes/CompleteTitles/os68.pdf), §§ 2385.7 to 2385.14 with their history; the PDF was created 2025-12-30 | `000e4e3b7b834aad968b862d35e24cf4b443af0877e17993163afb2b179ff901` |
| `os25-82-1.txt` | [Oklahoma Statutes Title 25, complete title](https://www.oklegislature.gov/OK_Statutes/CompleteTitles/os25.pdf), § 82.1 with its history; the PDF was created 2025-12-30 | `947138b719a29e19d5adde6da9b5ae184799cf1503419826ff7101d4d526ea30` |
| `hb2730-engr.txt` | [HB 2730, engrossed, passed the House February 24, 2026](https://oklegislature.gov/cf_pdf/2025-26%20ENGR/hB/HB2730%20ENGR.PDF), whole document | `fee55097c582d0fd0554c5a4a1058b2bb90aba06012995bdcfd435c75933bced` |
| `hb2730-status.txt` | [HB 2730 bill information, 2026 session](https://www.oklegislature.gov/BillInfo.aspx?Bill=HB2730&Session=2600), the History table, one action a line | `886e2e70e978166dfef7639127eede9926ccb14df3e216e2b59e903742cb6704` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
F=https://oklahoma.gov/content/dam/ok/en/tax/documents/forms
curl -o OW-8-ES.pdf      "$F/individuals/current/OW-8-ES.pdf"
curl -o OW-8-ES-SUP.pdf  "$F/individuals/current/OW-8-ES-SUP.pdf"
curl -o OW-8-P.pdf       "$F/other/OW-8-P.pdf"
curl -o OW-8-P-SUP-I.pdf "$F/individuals/current/OW-8-P-SUP-I.pdf"
curl -o 511-Pkt.pdf      "$F/individuals/current/511-Pkt.pdf"
curl -o os68.pdf https://www.oklegislature.gov/OK_Statutes/CompleteTitles/os68.pdf
curl -o os25.pdf https://www.oklegislature.gov/OK_Statutes/CompleteTitles/os25.pdf
curl -o hb2730-engr.pdf 'https://oklegislature.gov/cf_pdf/2025-26%20ENGR/hB/HB2730%20ENGR.PDF'
curl -o hb2730.html 'https://www.oklegislature.gov/BillInfo.aspx?Bill=HB2730&Session=2600'

pdftotext OW-8-ES.pdf ow-8-es-2026.txt
pdftotext -layout OW-8-ES-SUP.pdf ow-8-es-sup-2026.txt
pdftotext OW-8-P.pdf ow-8-p-2025.txt
pdftotext -layout OW-8-P-SUP-I.pdf ow-8-p-sup-i-2025.txt
pdftotext hb2730-engr.pdf hb2730-engr.txt

pdftotext 511-Pkt.pdf 511.txt
{ awk '/^\f?DUE DATE$/{f=1} f; /^original due date\.$/{if(f)exit}' 511.txt | tr -d '\f'
  awk '/^ESTIMATED INCOME TAX$/{f=1} f{print} /^placing taxable income on an annualized basis/{if(f)exit}' 511.txt
  awk '/^22 Oklahoma Estimated Tax Payments$/{f=1} /^23 Payment with Extension$/{if(f)exit} f' 511.txt
  awk '/^Underpayment of Estimated Tax Interest$/{f=1} /^41a & b Delinquent Penalty and Interest$/{if(f)exit} f' 511.txt
} > 511-pkt-2025-estimated.txt

pdftotext os68.pdf os68.txt
awk '/^§68-2385\.7\. Declaration of estimated tax\.$/{f=1} /^§68-2385\.15\.$/{if(f)exit} f' os68.txt > os68-2385.txt
pdftotext os25.pdf os25.txt
awk '/^§25-82\.1\. Designation and dates of holidays - Executive Order Acts/{f=1} /^§25-82\.2\. /{if(f)exit} f' os25.txt > os25-82-1.txt

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8", errors="replace").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py hb2730.html \
  | awk '/^History For HB 2730$/{f=1} /^Floor Amendments \(Senate\)$/{if(f)exit} f' \
  | tr '\n' ' ' | sed 's/ | | /\n/g' > hb2730-status.txt
echo >> hb2730-status.txt
```

The two worksheets with columns of factors and percentages are extracted with
their layout, so each row stays on one line. The rest are in reading order.
The 511 packet and the statute titles are cut to the sections used, bounded by
their own headings.

## No published worked example

None of OW-8-ES, OW-8-ES-SUP, OW-8-P, OW-8-P-SUP-I or the 511 instructions
works an example with figures. The tests are hand-worked cases, each worked in
its comment the way OW-8-P computes it.

## What is not here

- **The 2026 OW-8-P and OW-8-P-SUP-I.** Not published on 2026-10-20; the
  `current` and `other` paths serve the 2025 forms. Their arithmetic is used
  with the 2026 dates from OW-8-ES. The 2026 OW-8-ES-SUP matches the 2025
  OW-8-P-SUP-I's periods, factors and percentages.
- **68 O.S. §§ 2385.7, 2385.9 and 2385.13 on OSCN, and OAC 710:50-13 on
  rules.ok.gov.** Both sites returned a Cloudflare challenge on 2026-10-20 and
  were not bypassed, as `research/estimated-payments/ok.md` records. The
  statutes come from the Legislature's own Title 68 compilation instead. OAC
  710:50-13 is not checked in, and no figure rests on it.
- **The Governor's 2026 holiday Executive Order.** OMES's
  `.../omes/documents/2026StateHolidays.pdf` returned 404. The holidays are
  worked out from § 82.1(A)'s rules, which the test repeats; the extra
  Christmas day the order picks for 2026 is left out.
- **2026 session laws.** The Title 68 compilation's PDF was created
  2025-12-30. An amendment from the 2026 session other than HB 2730 was not
  searched for section by section; the research found none.
- **The OTC disaster relief release of 2025-09-30.** Not checked in; no
  figure rests on it.
