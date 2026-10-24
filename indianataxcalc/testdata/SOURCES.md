# Where these files came from

Every Indiana figure in this package is checked against a document the
Department of Revenue published. Those documents are PDFs; these files are text
extracted from them. This records which PDF, and how, so that anybody can redo
it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, extracted with `pdftotext` from poppler
24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `es40-2026.txt` | [Form ES-40, Estimated Tax Payment Form, 2026](https://forms.in.gov/Download.aspx?id=16906), State Form 46005 (R26 / 9-25), whole form, reading order | `adcebef24b498b0f62ee1fdd340a798f3a43bf129d192fce3559d6cb7104e350` |
| `it2210-2025.txt` | [Schedule IT-2210, 2025 Underpayment of Estimated Tax by Individuals](https://forms.in.gov/Download.aspx?id=16926), State Form 46002 (R27 / 9-25), form and instructions, reading order | `9611265ce223f620162c8e1c79e2d68265b92c99b595494107cf9402a7da61b4` |
| `it2210a-2025.txt` | [Schedule IT-2210A, 2025 Annualized Income Schedule](https://forms.in.gov/Download.aspx?id=16928), State Form 48437 (R25 / 9-25), form and instructions, page layout | `a3d9f70671a50ae1974ff41f9a89ea65c42e325b7c518735b2193ea94347dbe7` |
| `ib03.txt` | [Income Tax Information Bulletin #3, Payment of Indiana Estimated Tax by Individuals](https://www.in.gov/dor/files/ib03.pdf), March 2023, whole document, reading order | `1b137d0b2ec1b390116ebf830c1d08b82275295d6829df7e8d84387556e87d76` |
| `synopsis-2026-ic-6-3-4-4.1.txt` | [DOR Legislative Synopsis 2026](https://www.in.gov/dor/files/legislative-synopsis-2026.pdf), the IC 6-3-4-4.1 entry only | `4024add87015ad3ee16cc1b4a302a86256f3dd05ea4cf3eaf9813b2bb744e0b1` |
| `research-in-statute-quotes.txt` | Not a primary source: the Indiana Code quotes in `research/estimated-payments/in.md`, copied by hand and labelled as such. See below | none |

## Reproducing them

```bash
curl -L -o es40.pdf    'https://forms.in.gov/Download.aspx?id=16906'
curl -L -o it2210.pdf  'https://forms.in.gov/Download.aspx?id=16926'
curl -L -o it2210a.pdf 'https://forms.in.gov/Download.aspx?id=16928'
curl -L -o ib03.pdf    https://www.in.gov/dor/files/ib03.pdf
curl -L -o syn26.pdf   https://www.in.gov/dor/files/legislative-synopsis-2026.pdf

pdftotext es40.pdf es40-2026.txt
pdftotext it2210.pdf it2210-2025.txt
pdftotext -layout it2210a.pdf it2210a-2025.txt
pdftotext ib03.pdf ib03.txt
pdftotext syn26.pdf syn26.txt
awk '/Code: IC 6-3-4-4\.1$/{f=1} /Code: IC 6-3-4-4\.2$/{if(f)exit} f' syn26.txt \
  > synopsis-2026-ic-6-3-4-4.1.txt
```

The synopsis is 9,000 lines, so only the IC 6-3-4-4.1 entry is kept, bounded
by its own heading and the next one. The headings start with a form feed at a
page break, so the patterns are not anchored at the start of the line.

IT-2210A is extracted with its layout because its period dates, annualisation
factors and percentages sit in table rows that reading order splits apart.
Its instructions are in three columns, so phrases taken from them are short.

## The strongest checks are the instructions' examples

No Indiana document works a penalty to a dollar figure. The IT-2210 and
IT-2210A instructions do work the steps that decide it: Chris and Kate's $5,000
becoming $5,500 above $150,000 of AGI, Joe's late third payment moving to the
fourth column, Dana's $100 shortfall and $130 overpayment leaving $30, and
IT-2210A's $80 and $50. The tests reproduce each.

## What is not here

- **The Indiana Code.** iga.in.gov answered curl on 2026-10-20 with a
  691-byte page that only loads a script, both for
  `https://iga.in.gov/ic/2025/Title_6.html` and for the script itself, so IC
  6-3-4-4.1 and IC 6-8.1-10-2.1 are not checked in. The statute side of each
  divergence and rule cites the quotes in `research/estimated-payments/in.md`,
  whose verification read the 2025 Indiana Code. They are copied into
  `research-in-statute-quotes.txt`, headed as a copy, and the tests check
  against that file. The 10% rate is also on IT-2210 line 21 and in Bulletin
  #3, which are primary.
- **SEA 243 (2026).** The enrolled act was not read. The synopsis says it
  rewords the penalty reference in IC 6-3-4-4.1 from July 1, 2026 without
  changing the rate.
- **The 2026 IT-2210 and IT-2210A.** Not published. The 2025 forms' arithmetic
  is used with the 2026 due dates from ES-40.
- **An Indiana holiday list.** Bulletin #3 moves a date for a national or state
  holiday or a weekend. No holiday list is checked in, so only weekends are
  applied. The four 2026 due dates and February 1, 2027 are weekdays, and the
  tests check the computed dates against the printed ones.
