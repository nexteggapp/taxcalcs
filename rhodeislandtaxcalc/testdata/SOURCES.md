# Where these files came from

Every Rhode Island figure in this package is checked against a document the
Division of Taxation, the General Assembly or the Secretary of State
published. These files are text extracted from them. This records which
document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. The research file reports that tax.ri.gov's HTML pages return a bot
challenge; its PDFs answered curl with HTTP 200, and so did every statute
page, so nothing here rests on a quote copied from the research.

`T` is `https://tax.ri.gov/sites/g/files/xkgbur541/files` and `L` is
`https://webserver.rilegislature.gov/Statutes`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `ri-1040es-2026.txt` | [2026 RI-1040ES](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-01/2026%20RI-1040ES_w.pdf) (`T/2026-01/2026%20RI-1040ES_w.pdf`): page 1's three columns, its overpayment paragraph, and the page 2 worksheet lines 1 to 14 | `d643f8f49f33b171b40850146b52a89ad48fcd234af5a2a413d58998e98442ce` |
| `ri-2210-2025.txt` | [2025 Form RI-2210](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-01/2025%20RI-2210_w.pdf) (`T/2026-01/2025%20RI-2210_w.pdf`): the form, page layout, then the instructions page in two columns | `4464abe9e62d9f86fc681dc100783e68fb96305ec7a977039af7e6ea09d06b86` |
| `ri-2210a-2025.txt` | [2025 Form RI-2210A](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-01/2025%20RI-2210A_w.pdf) (`T/2026-01/2025%20RI-2210A_w.pdf`), page layout | `7b9bc9140493f29cf1c15749317658cd3fc156d69b915e659dab8a7704713338` |
| `ri-1040-2025-instructions.txt` | [2025 RI-1040 instructions](https://tax.ri.gov/sites/g/files/xkgbur541/files/2025-12/2025%201040R%20Instructions%20122025.pdf) (December 2025): the Estimated Income Tax Payments and Interest paragraphs | `17c4d5abe85fe61e69da5daa349865576b5a66425a147dd5686a922b0c208336` |
| `adv-2025-23.txt` | [ADV 2025-23, Interest rates posted for 2026](https://tax.ri.gov/sites/g/files/xkgbur541/files/2025-11/ADV_2025_23_Interest_Rates.pdf) (November 26, 2025), page layout | `4da15d89254c530710339abb3c39fc140afdd62c39ff272cc93e2cf3dbe99506` |
| `adv-2026-15.txt` | [ADV 2026-15, June Estimated Payment Due Date Reminder](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-06/ADV_2026_15_June_Estimated_Payments.pdf) (June 9, 2026) | `6a061cd263274ae612ace7b22e51bbdd508fd28b4cfe072dc4654c96dd99a85d` |
| `adv-2024-16.txt` | [ADV 2024-16, Division extends certain due dates to July 15](https://tax.ri.gov/sites/g/files/xkgbur541/files/2024-04/ADV_2024_16_Extension.pdf) (April 10, 2024), page layout | `1f6b01dd10130f328ca4aaebdf49e25b0b9eae42ad79de3a2db99fd3a22440fe` |
| `rigl-44-30-55.txt` | [R.I. Gen. Laws § 44-30-55](https://webserver.rilegislature.gov/Statutes/TITLE44/44-30/44-IV/44-30-55.htm), the section's text | `567f50a6e485a8e35867014272fa13768cb71a0c9827734a45895f919c797299` (the HTML) |
| `rigl-44-30-56.txt` | [§ 44-30-56](https://webserver.rilegislature.gov/Statutes/TITLE44/44-30/44-IV/44-30-56.htm) | `bccbf5e9938b68a5a00564b763edcdda6f82abea759b60e7cde3ed538bf40c9d` (the HTML) |
| `rigl-44-30-84.txt` | [§ 44-30-84](https://webserver.rilegislature.gov/Statutes/TITLE44/44-30/44-VI/44-30-84.htm) | `2a1c9334f06b531c01398c5b31e27a0068bffaf2ac59171e1acc8f384ef9c5c6` (the HTML) |
| `rigl-44-30-91.txt` | [§ 44-30-91](https://webserver.rilegislature.gov/Statutes/TITLE44/44-30/44-VI/44-30-91.htm) | `52e9da3a8cecce274d5849160687012c973e5eaddd617446426048d26c1205c3` (the HTML) |
| `rigl-44-1-7.txt` | [§ 44-1-7](https://webserver.rilegislature.gov/Statutes/TITLE44/44-1/44-1-7.htm) | `6457da825b112d708404b5c393fc936df060b0621e5307f87169b2818670a9f5` (the HTML) |
| `rigl-25-1-1.txt` | [§ 25-1-1](https://webserver.rilegislature.gov/Statutes/TITLE25/25-1/25-1-1.htm), general holidays | `eec7595f5951013cc63870e77ff95b8d316bb8612dc9710defc5d4cde23e170e` (the HTML) |
| `ricr-280-20-55-1.txt` | [280-RICR-20-55-1, Estimated Payments](https://rules.sos.ri.gov/regulations/part/280-20-55-1) (effective 01/04/2022), the regulation text | `d70758508ddd4f8380ad60eb098e8e100f759cec64b8c9c90fcdb710e7366f5b` (the HTML) |

An HTML page's hash records what was fetched. The page carries navigation that
can change without its content changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
T=https://tax.ri.gov/sites/g/files/xkgbur541/files
L=https://webserver.rilegislature.gov/Statutes
curl -o 1040es.pdf      "$T/2026-01/2026%20RI-1040ES_w.pdf"
curl -o ri2210.pdf      "$T/2026-01/2025%20RI-2210_w.pdf"
curl -o ri2210a.pdf     "$T/2026-01/2025%20RI-2210A_w.pdf"
curl -o 1040r.pdf       "$T/2025-12/2025%201040R%20Instructions%20122025.pdf"
curl -o adv2025-23.pdf  "$T/2025-11/ADV_2025_23_Interest_Rates.pdf"
curl -o adv2026-15.pdf  "$T/2026-06/ADV_2026_15_June_Estimated_Payments.pdf"
curl -o adv2024-16.pdf  "$T/2024-04/ADV_2024_16_Extension.pdf"
curl -o s44-30-55.htm   "$L/TITLE44/44-30/44-IV/44-30-55.htm"
curl -o s44-30-56.htm   "$L/TITLE44/44-30/44-IV/44-30-56.htm"
curl -o s44-30-84.htm   "$L/TITLE44/44-30/44-VI/44-30-84.htm"
curl -o s44-30-91.htm   "$L/TITLE44/44-30/44-VI/44-30-91.htm"
curl -o s44-1-7.htm     "$L/TITLE44/44-1/44-1-7.htm"
curl -o s25-1-1.htm     "$L/TITLE25/25-1/25-1-1.htm"
curl -L -o ricr.html    https://rules.sos.ri.gov/regulations/part/280-20-55-1

{
  for c in "0 210" "210 192" "402 210"; do set -- $c
    pdftotext -f 1 -l 1 -x $1 -y 0 -W $2 -H 350 1040es.pdf -; done
  pdftotext -f 1 -l 1 -x 0 -y 360 -W 225 -H 85 1040es.pdf - \
    | awk '/^RECORD OF ESTIMATED PAYMENTS$/{exit} 1'
  pdftotext -f 2 -l 2 -layout 1040es.pdf - \
    | awk '/PART 1 ESTIMATED RHODE ISLAND INCOME TAX WORKSHEET/{f=1} f; /14\. Divide line 13/{if(f)exit}'
} > ri-1040es-2026.txt
{
  pdftotext -f 1 -l 2 -layout ri2210.pdf -
  pdftotext -f 3 -l 3 -x 0 -y 0 -W 303 -H 792 ri2210.pdf -
  pdftotext -f 3 -l 3 -x 303 -y 0 -W 309 -H 792 ri2210.pdf -
} > ri-2210-2025.txt
pdftotext -layout ri2210a.pdf ri-2210a-2025.txt
pdftotext 1040r.pdf 1040r.txt
awk '/^ESTIMATED INCOME TAX PAYMENTS$/{f=1} f; /must make estimated tax payments/{if(f){f=0}}
     /^INTEREST$/{g=1} g; /12% \(0.1200\) per annum/{if(g)exit}' 1040r.txt > ri-1040-2025-instructions.txt
pdftotext -layout adv2025-23.pdf adv-2025-23.txt
pdftotext -layout adv2024-16.pdf adv-2024-16.txt
pdftotext adv2026-15.pdf adv-2026-15.txt

cat > h2t.py <<'PY'
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
for s in 44-30-55 44-30-56 44-30-84 44-1-7 44-30-91 25-1-1; do
  python3 h2t.py s$s.htm \
    | awk -v s="§ $s." 'index($0,s)==1{f=1} f; /^History of Section\.$/{f=0}' | sed '$d' > rigl-$s.txt
done
python3 h2t.py ricr.html | awk '/1\.1Purpose/{f=1} /^Title \|$/{f=0} f' \
  | sed 's/^My Page Title//' > ricr-280-20-55-1.txt
```

The RI-1040ES front page and the RI-2210 instructions are set in columns, so
they are cut into columns with `-x`, `-y`, `-W` and `-H`. The RI-2210 form
pages and the advisories keep their rows only with `-layout`.

## There is no worked example

Neither the RI-1040ES nor the RI-2210 works a case with a stated answer. The
nearest thing is the RI-2210A shortcut: 7.9808% of the underpayment, less
0.000329 a day paid early. `TestEstimatedShortcutFactorFollowsFromTheRate`
checks that both follow from the 12% rate and the day counts the rules
compute for a filer who paid nothing: 12% × (365 + 304 + 212 + 90) ÷ 365 ÷ 4
and 12% ÷ 365. The 2025 RI-2210A is for 2025's dates; 2026's give the same
day counts.

## Why the 2025 RI-2210

No 2026 RI-2210 was published by 2026-10-20. The 2025 form's arithmetic is
used with the 2026 dates from the RI-1040ES, and its "January 31, 2026" is
read as January 31, 2027. ADV 2025-23 sets 12% for 2026, the same as the 2025
form uses, and its table shows 12% for every year from 2023.

## What is not here

- **The rate from January 1, 2027.** § 44-1-7(b) sets it on that day from the
  prime rate on October 1, 2026, and never below 12%. It is not published, so
  the penalty counts only the days of 2026 and reports the gap in `Required`.
- **The 2024 RI-2210.** The research cites it for a schedule changed by storm
  relief in 2024. It sets no 2026 figure.
- **The 2025 RI-1040NR instructions.** They repeat the RI-1040 booklet's
  estimate and interest paragraphs, which are checked in from the resident
  booklet.
- **A proclamation of Thanksgiving, and whether 2026 holds a general election
  of state officers.** § 25-1-1 makes both holidays. Neither falls on a date
  these rules move, so the calendar leaves them out.
