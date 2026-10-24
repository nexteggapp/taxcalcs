# Where these files came from

Every Colorado figure in this package is checked against a document the
Colorado Department of Revenue (CDOR) or the Colorado General Assembly
published. These files are text extracted from them. This records which
document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, with no special headers. PDFs were
extracted with `pdftotext` from poppler 24.02.0, and HTML pages reduced to
text with the Python 3.12.3 script below.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `dr0104ep-2026.txt` | [DR 0104EP, Individual Estimated Income Tax, 2026 (06/10/25)](https://tax.colorado.gov/sites/default/files/documents/DR_0104EP_2026.pdf), whole document, reading order | `2b4e33fe2bdc4f5e3b324687f11ef7a0b22698673341a7a8b07764203946b12c` |
| `dr0204-2025.txt` | [DR 0204, Computation of Penalty Due Based on Underpayment of Colorado Individual Estimated Tax, 2025 (09/25/25)](https://tax.colorado.gov/sites/default/files/documents/DR0204_2025.pdf), whole document, reading order | `9027688e12dfe977c6bfe08f09a8bed759ed3391ad56e812d99473a1989111d5` |
| `dr0204-2025-annualized.txt` | the same PDF, Part 4, page layout | same |
| `crs-39-22-605.txt` | [Colorado Revised Statutes 2024, title 39](https://content.leg.colorado.gov/sites/default/files/images/olls/crs2024-title-39.pdf) (uncertified printout), § 39-22-605 | `4a7c73c83e82f237cb94a39826d380466d1fc8b9ecb62d52cc36323f7c775a34` |
| `crs-39-21-109.txt` | the same PDF, § 39-21-109 | same |
| `crs-39-21-110.5.txt` | the same PDF, § 39-21-110.5 | same |
| `cdor-estimated-payments.txt` | [Individual Income Tax \| Estimated Payments](https://tax.colorado.gov/individual-income-tax-estimated-payments), Due Dates to the end of the page's content | `4f2e76de01175456df19a6e65e1f203e28428afd7b2b85bbf17a38eb263ab383` (the HTML) |
| `cdor-penalties-and-interest.txt` | [Tax Topics: Penalties and Interest](https://tax.colorado.gov/tax-topics-penalties-and-interest), Interest on late payments, with the rate table and the worked example | `9e4eb02449275d7218042c60c0842366f20027bf815a2441c9fc919e20b57c00` (the HTML) |
| `redbook-2025-articles-21-22.txt` | [Red Book 2025](https://content.leg.colorado.gov/node/3099561), the sections of title 39, articles 21 and 22, the 2025 session changed | `755e7195966ae221c4689358a5ac840174ac190fb52caba84f647e0268ac2d9e` (the HTML) |
| `redbook-2026-articles-21-22.txt` | [Red Book 2026](https://content.leg.colorado.gov/agencies/office-legislative-legal-services/red-book-2026), the same for the 2026 session | `c04f3556b57be94ae9959dbc6f43fd528af5d0c0b325dddff098abe4a74f141c` (the HTML) |

An HTML page's hash records what was fetched. The page carries navigation and
scripts that change without its content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

The research file says tax.colorado.gov blocked curl with a bot challenge.
On 2026-10-20 it did not: plain curl fetched every CDOR document above.

## Reproducing them

```bash
curl -o DR_0104EP_2026.pdf https://tax.colorado.gov/sites/default/files/documents/DR_0104EP_2026.pdf
curl -o DR0204_2025.pdf    https://tax.colorado.gov/sites/default/files/documents/DR0204_2025.pdf
curl -o crs2024-title-39.pdf https://content.leg.colorado.gov/sites/default/files/images/olls/crs2024-title-39.pdf
curl -o estimated-payments.html https://tax.colorado.gov/individual-income-tax-estimated-payments
curl -o penalties-and-interest.html https://tax.colorado.gov/tax-topics-penalties-and-interest
curl -o redbook2025.html https://content.leg.colorado.gov/node/3099561
curl -o redbook2026.html https://content.leg.colorado.gov/agencies/office-legislative-legal-services/red-book-2026

pdftotext DR_0104EP_2026.pdf dr0104ep-2026.txt
pdftotext DR0204_2025.pdf dr0204-2025.txt
pdftotext -layout DR0204_2025.pdf dr0204.L.txt
awk '/^Part 4 — Annualized Installment Method Schedule/{f=1} f' dr0204.L.txt \
  > dr0204-2025-annualized.txt

pdftotext crs2024-title-39.pdf crs.txt
awk '/^39-22-605\. Failure by individual/{f=1} /^39-22-606\. /{f=0} f' crs.txt > crs-39-22-605.txt
awk '/^39-21-109\. Interest on underpayment/{f=1} /^39-21-110\. /{f=0} f' crs.txt > crs-39-21-109.txt
awk '/^39-21-110\.5\. Rate of interest/{f=1} /^39-21-111\. /{f=0} f' crs.txt > crs-39-21-110.5.txt

cat > html2text.py <<'PY'
# Reduces a CDOR HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py estimated-payments.html \
  | awk '/^Due Dates$/{f=1} /^Return to top$/{if(f)exit} f' > cdor-estimated-payments.txt
python3 html2text.py penalties-and-interest.html \
  | awk '/^Interest on late payments$/{f=1} /^International Fuel Tax Agreement \(IFTA\)$/{if(f)exit} f' \
  > cdor-penalties-and-interest.txt
python3 html2text.py redbook2025.html | grep '^39-2[12]-' > redbook-2025-articles-21-22.txt
python3 html2text.py redbook2026.html | grep '^39-2[12]-' > redbook-2026-articles-21-22.txt
```

The statute PDF is 1,163 pages, so only the three sections are kept, bounded
by the next section's heading. The Red Books list every section a session
changed; only the lines for title 39, articles 21 and 22, are kept, and
`TestEstimatedStatuteUnchangedSince2024` checks that neither lists
§ 39-22-605, § 39-21-109 or § 39-21-110.5. DR 0204 Part 4 is a table whose
columns reading order scatters, so it is extracted again with its layout.

## What is the strong check

Colorado publishes no worked example of the estimated tax penalty. CDOR's
penalties and interest page works one example of interest on a late payment:
$200 due April 15, 2022, paid February 22, 2024, at 3%, 5% and 8%, is $16.59.
It is the same method DR 0204 lines 13 to 18 use, each calendar year at its
own rate over 365 or 366 days, and `TestInterestMatchesTheDepartmentsExample`
reproduces it to the cent. Everything else is hand-written cases, worked by
hand in the test comments.

## What is not here

- **The 2026 DR 0204.** Not published on 2026-10-20. The 2025 form's
  arithmetic is used with the 2026 figures. Its lines 15 and 18 print 12% and
  11%, the 2025 and 2026 rates; a 2026 underpayment is charged 11% and 10%
  from CDOR's table, and the divergence is reported.
- **The 2025 statute.** The 2025 C.R.S. title 39 PDF returned 404 to the
  research. The 2024 text is used; the Red Books show the 2025 and 2026
  sessions did not change the sections used.
- **A department rule on the annualised method.** C.R.S. § 39-22-605(6)(d)
  leaves the method to rules the department promulgates. None is checked in;
  DR 0204 Part 4 is followed.
- **A Colorado holiday list.** None is checked in. Every 2026 due date
  DR 0104EP prints is a weekday, so only weekends are applied, and the tests
  check the computed dates against the printed ones.
- **Denver's occupational privilege tax guide.** The research read it to
  confirm it takes no estimated payments; nothing here computes it, so it is
  not checked in.
