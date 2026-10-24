# Where these files came from

Every Wisconsin figure in this package is checked against a document the
Department of Revenue or the Legislative Reference Bureau published. Those
documents are PDFs; these files are text extracted from them. This records
which PDF, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, extracted with `pdftotext` from poppler
24.02.0. `R` is `https://www.revenue.wi.gov`, and `L` is
`https://docs.legis.wisconsin.gov/statutes/statutes`. The statute chapters
are the "2023-24 Wisconsin Statutes updated through 2025 Wis. Act 247 ...
(Published 10-1-26)".

| File | Source | sha256 of the PDF |
|---|---|---|
| `form-1-es-inst-2026.txt` | [2026 Form 1-ES Instructions (D-101A, R. 1-26)](https://www.revenue.wi.gov/TaxForms2026/2026-Form1-ES-Inst.pdf) (`R/TaxForms2026/2026-Form1-ES-Inst.pdf`), whole document, reading order | `b419054ac3b9ce7c714e40604d93448b8824648be8442862504f12f2eb1374b2` |
| `form-1-es-inst-2026-dates.txt` | the same PDF, the table of later first payment dates, page layout | same |
| `schedule-u-2025.txt` | [2025 Schedule U (D-104, R. 05-25)](https://www.revenue.wi.gov/TaxForms2025/2025-ScheduleU.pdf) (`R/TaxForms2025/2025-ScheduleU.pdf`), page layout | `90a843565015d1a81ff0a4a857ee7b7617cf57d5a0e51578691cc21db9500506` |
| `schedule-u-inst-2025.txt` | [2025 Schedule U Instructions (D-114, R. 11-25)](https://www.revenue.wi.gov/TaxForms2025/2025-ScheduleU-Inst.pdf) (`R/TaxForms2025/2025-ScheduleU-Inst.pdf`), reading order | `f4c1430b92a26f1f0b180cccbc8829432706f5d1ad963b4b281368863273ed0b` |
| `wis-stat-71-09.txt` | [Wis. Stat. ch. 71](https://docs.legis.wisconsin.gov/statutes/statutes/71.pdf) (`L/71.pdf`), § 71.09, reading order | `92b5decd9bec8cb924548c552acddda60c45b1c0911910fce39be779bdb61da6` |
| `wis-stat-71-84.txt` | the same PDF, § 71.84 | same |
| `wis-stat-990-001-4.txt` | [Wis. Stat. ch. 990](https://docs.legis.wisconsin.gov/statutes/statutes/990.pdf) (`L/990.pdf`), § 990.001(4) | `07dabdb19d57ff6e3146f383879abba8ad3d500bcae8b602ae28975252ab37db` |
| `wis-stat-995-20.txt` | [Wis. Stat. ch. 995](https://docs.legis.wisconsin.gov/statutes/statutes/995.pdf) (`L/995.pdf`), § 995.20 | `d17d0bc83df4167ccef332b54dd5fb56c78a890d96727d5362cb3678835fe9cf` |
| `wis-stat-5-02.txt` | [Wis. Stat. ch. 5](https://docs.legis.wisconsin.gov/statutes/statutes/5.pdf) (`L/5.pdf`), § 5.02(5) and (12s), which date the two election days § 995.20 names | `a10e9e6ae074474799ce8197f7d0d325e87a104996df4f9a1d6f8b7ac65eaf56` |

## Reproducing them

```bash
R=https://www.revenue.wi.gov
L=https://docs.legis.wisconsin.gov/statutes/statutes
curl -o 1-es-inst.pdf "$R/TaxForms2026/2026-Form1-ES-Inst.pdf"
curl -o schedule-u.pdf "$R/TaxForms2025/2025-ScheduleU.pdf"
curl -o schedule-u-inst.pdf "$R/TaxForms2025/2025-ScheduleU-Inst.pdf"
for c in 5 71 990 995; do curl -o "ch$c.pdf" "$L/$c.pdf"; done

pdftotext 1-es-inst.pdf form-1-es-inst-2026.txt
pdftotext -layout 1-es-inst.pdf 1-es-inst.L.txt
awk '/If the requirement to pay/{f=1} f; /January 15, 2027 *all/{if(f)exit}' 1-es-inst.L.txt \
  > form-1-es-inst-2026-dates.txt

pdftotext -layout schedule-u.pdf schedule-u-2025.txt
pdftotext schedule-u-inst.pdf schedule-u-inst-2025.txt

for c in 5 71 990 995; do pdftotext "ch$c.pdf" "ch$c.txt"; done
awk '/^71\.09 Payment of estimated taxes\./{f=1} /^71\.10 General provisions\./{exit} f' ch71.txt \
  > wis-stat-71-09.txt
awk '/^71\.84 Addition to the tax\./{f=1} /^71\.85 General provisions\./{exit} f' ch71.txt \
  > wis-stat-71-84.txt
awk '/^\(4\) TIME, HOW COMPUTED\./{f=1} /^\(5\) STATUTORY REFERENCES\./{exit} f' ch990.txt \
  > wis-stat-990-001-4.txt
awk '/^995\.20 Legal holidays\./{f=1} f; f && /2017 a\. 207/{exit}' ch995.txt \
  > wis-stat-995-20.txt
awk '/^\(5\) “General election” means/{f=1} /^\(6\) “Governing body”/{f=0}
     /^\(12s\) “Partisan primary” means/{f=1} /^\(13\) “Political party”/{f=0} f' ch5.txt \
  > wis-stat-5-02.txt
```

The 1-ES instructions and the Schedule U instructions are set in columns, so
they are extracted in reading order. The 1-ES table of later payment dates
loses its rows in reading order, so it is extracted again with its layout,
bounded by its own heading. Schedule U is a form of numbered lines and is
extracted with its layout.

The statute chapters are two-column PDFs. In reading order a section's text
runs together with the page headers and, at a page break, with a few lines
of the next section, which is why `wis-stat-71-09.txt` ends with part of
§ 71.10. Each extract is bounded by the section's own heading and the next
section's.

The statute pages at `docs.legis.wisconsin.gov/document/statutes/71.09` and
the like serve HTML. The chapter PDFs carry the same text and a publication
date, so those are what is checked in.

## The worked examples are the strong check

The Schedule U instructions work three examples of carrying an overpayment
forward (line 23) and back (line 22), with the amounts each column gets.
`TestEstimatedScheduleUExamples` builds each from payments against $1,000
installments and checks the shortfalls and the interest that follow. The
second example tells this order apart from the federal one, which would pay
April first.

## What is not here

- **The 2026 Schedule U and its instructions.** Not published:
  `R/TaxForms2026/2026-ScheduleU.pdf` returns 404. The 2025 form's arithmetic
  is used with the 2026 dates. Its January waiver date, February 2, 2026, is
  January 31, 2026 moved off a Saturday; the 2026 date, February 1, 2027, is
  January 31, 2027 moved off a Sunday under § 990.001(4)(b).
- **The 2026 Form 1-ES voucher.** `R/TaxForms2026/2026-Form1-ES.pdf` returns
  404. The instructions carry every figure used.
- **A rate change.** § 71.84(1) fixes the rate at 12%. Its history ends at
  2017 Wis. Act 2, and no published rate change falls in the 2026 window.
