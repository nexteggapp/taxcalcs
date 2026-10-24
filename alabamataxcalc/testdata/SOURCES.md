# Where these files came from

Every Alabama figure in this package is checked against a document the
Alabama Department of Revenue (ADOR) published, or against the statute text
from the Alabama Legislature or the U.S. Government Publishing Office. These
files are text extracted from those documents. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages and the Legislature's JSON reduced to text with the
Python 3.12.3 scripts below. `B` is
`https://www.revenue.alabama.gov/wp-content/uploads/2026/01`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `al-40es-2026-instructions.txt` | [Form 40ES Instructions, 2026](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/26f40esinstr.pdf) (`B/26f40esinstr.pdf`), whole document, reading order | `8ca79c2f6bcb59b5b3f0db7e011850bc15594a6aa0b331d1fa217da8fc42bf5e` |
| `al-40es-2026-worksheet.txt` | [Form 40ES worksheet and voucher, 2026](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/26f40esblk.pdf) (`B/26f40esblk.pdf`), the worksheet and record of payments, page layout | `1df0e56d8989e115e15442f0544f1c2f3e8adeea55979bd17b11aee90cf4ed50` |
| `al-2210al-2025.txt` | [Form 2210AL, 2025](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/25f2210al.pdf) (`B/25f2210al.pdf`), whole form, page layout | `73a8e73199278f33ebc754f5970ef4126a09d4e9c4c64fdb79a1ed0b3a82f9f8` |
| `al-2210al-2025-instructions.txt` | [Instructions for Form 2210AL, 2025](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/25f2210al_inst.pdf) (`B/25f2210al_inst.pdf`), whole document, reading order | `565204b4a8350cb85f842600b507da1438a8e21082d29d4c769651cef09782e3` |
| `al-interest-rates.txt` | [ADOR, Quarterly Interest Rates](https://www.revenue.alabama.gov/assessments/quarterly-interest-rates/), from the formula example to the statement of authority | `bfad72a30b8e7d3098197914c877abc58735c5fdd7580298e1af198486468f38` (the HTML) |
| `al-code-40-18-80.txt` | [Code of Ala. § 40-18-80](https://alison.legislature.state.al.us/code-of-alabama?section=40-18-80), from the Legislature's GraphQL endpoint | `9ab4ce7830b58420dc04c012eb9a225ad48b94d6f71a913684b6e93a33ed2260` (the JSON) |
| `al-code-40-1-44.txt` | [Code of Ala. § 40-1-44](https://alison.legislature.state.al.us/code-of-alabama?section=40-1-44), subsection (a) | `23c77b7ce351153c762913c5d0628bdbfb4aee9a1570c3257c430bfaeabd6008` (the JSON) |
| `al-code-1-3-8.txt` | [Code of Ala. § 1-3-8](https://alison.legislature.state.al.us/code-of-alabama?section=1-3-8), the State holidays, subsections (a) and (b) | `26b42524d2ced975e724f5798e39c32400556a8098dda4fe005c09406a8e77c9` (the JSON) |
| `usc-6654.txt` | [26 U.S.C. § 6654, United States Code, 2024 Edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleF-chap68-subchapA-partI-sec6654.htm), the section text without its notes | `4d7fcd8ee19a8ba3f5502404a47a9f839378abf0d9a4c01d0adcc00483d480ff` (the HTML) |

An HTML page's or JSON response's hash records what was fetched. A page
carries navigation and scripts that change without its content changing, so a
later fetch can hash differently and extract the same; diff the extract rather
than the hash.

## Reproducing them

```bash
B=https://www.revenue.alabama.gov/wp-content/uploads/2026/01
for f in 26f40esinstr 26f40esblk 25f2210al 25f2210al_inst; do curl -L -o $f.pdf "$B/$f.pdf"; done
curl -L -o rates.html https://www.revenue.alabama.gov/assessments/quarterly-interest-rates/
curl -L -o usc6654.html "https://www.govinfo.gov/link/uscode/26/6654?link-type=html"
for s in 40-18-80 40-1-44 1-3-8; do
  curl -H 'Content-Type: application/json' -o $s.json \
    -d "{\"query\":\"query codeOfAlabamaSection(\$displayId: String!) { codesOfAlabama(where: { type: { eq: Section }, displayId: { eq: \$displayId } }, includeAllVersions: true) { data { codeId displayId title content history effectiveDate } } }\",\"variables\":{\"displayId\":\"$s\"}}" \
    https://alison.legislature.state.al.us/graphql
done

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
cat > json2text.py <<'PY'
# Writes an ALISON codeOfAlabamaSection response as text: the title, one line
# per <p> of the content, then the history.
import html, json, re, sys
d = json.load(open(sys.argv[1], encoding="utf-8"))["data"]["codesOfAlabama"]["data"][0]
out = [d["title"]]
for p in re.split(r"(?i)</p>", d["content"]):
    p = html.unescape(re.sub(r"<[^>]+>", "", p)).strip()
    if p:
        out.append(re.sub(r"\s+", " ", p))
out.append("History: " + re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", d["history"] or ""))).strip())
sys.stdout.write("\n".join(out) + "\n")
PY

pdftotext 26f40esinstr.pdf al-40es-2026-instructions.txt
pdftotext -layout 26f40esblk.pdf 26f40esblk.L.txt
awk '/WORKSHEET – KEEP FOR YOUR RECORDS/{f=1} f; /it is important that you maintain this record/{if(f)exit}' 26f40esblk.L.txt \
  > al-40es-2026-worksheet.txt
pdftotext -layout 25f2210al.pdf al-2210al-2025.txt
pdftotext 25f2210al_inst.pdf al-2210al-2025-instructions.txt

python3 html2text.py rates.html \
  | awk '/^Formula example/{f=1} f; /Section 40-5-9\.$/{if(f)exit}' > al-interest-rates.txt
python3 html2text.py usc6654.html \
  | awk '/^§6654\. Failure by individual/{f=1} /^\(Aug\. 16, 1954/{exit} f' > usc-6654.txt

python3 json2text.py 40-18-80.json > al-code-40-18-80.txt
python3 json2text.py 40-1-44.json | awk 'NR==1 || /^\(a\)/' > al-code-40-1-44.txt
python3 json2text.py 1-3-8.json | awk '/^\(c\)/{exit} 1' > al-code-1-3-8.txt
```

The 40ES instructions and the 2210AL instructions are set in columns, so they
are extracted in reading order. Form 2210AL keeps its layout, because its
columns and percentages sit in rows. The Legislature's site renders the Code
with JavaScript; the GraphQL query is the one its own page sends, and it
returns the same text.

## There is no worked example

Neither Form 2210AL nor its instructions works an example with an answer, and
the rates page gives only the formula. The penalty tests are worked by hand in
their comments from the form's line 11 and the published rates.

## What is not here

- **The 2026 Form 2210AL.** Not published when these were fetched; the 2025
  form, posted in January 2026, is the latest. Its arithmetic is used with the
  2026 figures from the 40ES and the 2026 rates.
- **The rate from January 1, 2027.** Not on the rates page. The penalty reports
  the gap in `Required`.
- **Code of Ala. § 40-2A-11.** The 2210AL instructions cite it; the research
  found only a general reasonable-cause waiver in subsection (h), and no
  figure here depends on it.
- **Thanksgiving.** § 1-3-8 makes it a State holiday "as designated by the
  Governor", with no fixed date. No 2026 due date is in November, so it is not
  needed.
