# Where these files came from

Every Delaware figure in this package is checked against a document the
Delaware Division of Revenue or the Delaware Code Online published. These
files are text extracted from them. This records which document, and how, so
that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, with no special headers. PDFs were
extracted with `pdftotext` from poppler 24.02.0, and the Delaware Code's HTML
pages reduced to text with the Python 3.12.3 script below. `R` is
`https://revenuefiles.delaware.gov/2025/PITForms_Instructions`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `pit-est-2026.txt` | [PIT-EST Instructions, Declaration of Estimated Income Tax for Individuals, 2026 (revised 11/10/25)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/Instructions/PIT-EST_Instructions_2026-01.pdf) (`R/Instructions/PIT-EST_Instructions_2026-01.pdf`), whole document, reading order | `08a8e73cf064f2599fd476d199581f71cf0f224da36525a68debc475bc31b8a5` |
| `pit-est-2026-voucher.txt` | [PIT-EST voucher, 2026, quarter 1 (revision 20250516)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/PIT-EST_2026-01_PaperInteractiveIPM.pdf) (`R/PIT-EST_2026-01_PaperInteractiveIPM.pdf`), whole document, page layout | `edbd5a675fc7a732601eb1a3d7d5267d135e8f0fe2d4b37973a4564594ccaf5e` |
| `pit-und-2025.txt` | [PIT-UND, Delaware Underpayment of Estimated Taxes, 2025 (revision 20250724)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/PIT-UND_2025-01_PaperInteractiveIPM.pdf) (`R/PIT-UND_2025-01_PaperInteractiveIPM.pdf`), whole form, page layout | `a485b8212e24a1b0faee8f5ffba2a3d7245d66cc92ab592d31084b6a9baf6654` |
| `pit-und-2025-instructions.txt` | [PIT-UND Instructions, 2025 (revised 11/10/25)](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/Instructions/PIT-UND_Instructions_2025-01.pdf) (`R/Instructions/PIT-UND_Instructions_2025-01.pdf`), whole document, reading order | `442595382e25b268c0bf70c45677138d09644393775d4f26a3136fcc48039751` |
| `pit-res-2025-estimated.txt` | [PIT-RES Resident Instructions, 2025](https://revenuefiles.delaware.gov/2025/PITForms_Instructions/Instructions/PIT-RES_Instructions_2025-01.pdf) (`R/Instructions/PIT-RES_Instructions_2025-01.pdf`), Requirement to File Estimated Taxes to the end of page 4 | `b8b1dc59b9ce12b406e12a21533de0b72e5adde33f4a672859914660b08768b8` |
| `delcode-30-1168-1170.txt` | [30 Del. C. ch. 11, subch. VIII](https://delcode.delaware.gov/title30/c011/sc08/index.html), §§ 1168 to 1170 | `c6a56bc81b2122dadd9c6eacf1fb865f5559787c2683b21f034b252ffa865f97` (the HTML) |
| `delcode-30-533-535.txt` | [30 Del. C. ch. 5, subch. III](https://delcode.delaware.gov/title30/c005/sc03/index.html), §§ 533 to 535 | `6cc8f3123b618dffcfb4f58a701012f581d037b95c876510ea118458bd7804ef` (the HTML) |
| `delcode-30-551.txt` | [30 Del. C. ch. 5, subch. IV](https://delcode.delaware.gov/title30/c005/sc04/index.html), § 551 | `bd48b103636c83a68031b9923ff0ba87dad7ff78f673f064c9aa29af7912e3a5` (the HTML) |

An HTML page's hash records what was fetched. The page carries navigation that
can change without the statute changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

The research file says the revenuefiles.delaware.gov PDFs reject requests
without browser headers. On 2026-10-20 they did not: plain curl fetched every
document above.

## Reproducing them

```bash
R=https://revenuefiles.delaware.gov/2025/PITForms_Instructions
curl -O "$R/Instructions/PIT-EST_Instructions_2026-01.pdf"
curl -O "$R/PIT-EST_2026-01_PaperInteractiveIPM.pdf"
curl -O "$R/PIT-UND_2025-01_PaperInteractiveIPM.pdf"
curl -O "$R/Instructions/PIT-UND_Instructions_2025-01.pdf"
curl -O "$R/Instructions/PIT-RES_Instructions_2025-01.pdf"
curl -o delcode-c011-sc08.html https://delcode.delaware.gov/title30/c011/sc08/index.html
curl -o delcode-c005-sc03.html https://delcode.delaware.gov/title30/c005/sc03/index.html
curl -o delcode-c005-sc04.html https://delcode.delaware.gov/title30/c005/sc04/index.html

pdftotext PIT-EST_Instructions_2026-01.pdf pit-est-2026.txt
pdftotext -layout PIT-EST_2026-01_PaperInteractiveIPM.pdf pit-est-2026-voucher.txt
pdftotext -layout PIT-UND_2025-01_PaperInteractiveIPM.pdf pit-und-2025.txt
pdftotext PIT-UND_Instructions_2025-01.pdf pit-und-2025-instructions.txt
pdftotext PIT-RES_Instructions_2025-01.pdf pit-res.txt
awk '/Requirement to File Estimated Taxes$/{f=1} f; /^Page 4$/{if(f)exit}' pit-res.txt \
  > pit-res-2025-estimated.txt

cat > html2text.py <<'PY'
# Reduces a Delaware Code HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<span style='vertical-align:super", " <span", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py delcode-c011-sc08.html \
  | awk '/^1168\. Time and place/{f=1} /^1171\. /{f=0} f' > delcode-30-1168-1170.txt
python3 html2text.py delcode-c005-sc03.html \
  | awk '/^533\. Interest on underpayment/{f=1} /^536\. /{f=0} f' > delcode-30-533-535.txt
python3 html2text.py delcode-c005-sc04.html \
  | awk '/^551\. Timely mailing/{f=1} /^552\. /{f=0} f' > delcode-30-551.txt
```

The PIT-UND form is a grid whose columns reading order scatters, so it is
extracted with its layout; so is the voucher. The instruction documents are
extracted in reading order. The PIT-UND instructions are set in two columns
that pdftotext interleaves in places, so the tests match short phrases there.
The Delaware Code prints 1 1/2 as a raised 1 over a lowered 2; the script puts
a space before the raised digit so the text reads "1 1/2%". Only the sections
the rules cite are kept, bounded by the next section's heading.

## What is the strong check

Delaware publishes no worked example with a stated answer. The note under
PIT-UND line 43 illustrates split payments with "48 days" and "93 days" that
match neither the form's dates nor the due dates, so it is not used. Every
figure is compared against these extracts, and the penalty arithmetic is
hand-written cases worked in the test comments.

## What is not here

- **The 2026 PIT-UND.** Not published on 2026-10-20. The 2025 form's
  arithmetic is used with the 2026 dates. Its line 39 pattern, 5/1 for the
  first installment and the due date for the rest, is carried to 2026 as
  May 1, 2026, and the result asks about it.
- **Session laws.** The research file read the 85 Delaware Laws chapter list
  and found no 2025 or 2026 act changing §§ 535, 1169 or 1170. That list gives
  only titles, so it is not checked in. `TestEstimatedStatuteHasNoRecentAmendment`
  checks instead that the Delaware Code's own history notes for the extracted
  sections list no 85 Del. Laws chapter.
- **A Delaware holiday list.** None is checked in. Every 2026 due date the
  forms print, and April 30, 2027, is a weekday, so only weekends are applied,
  and the tests check the computed dates against the printed ones.
- **The 2024 PIT-UND, the PIT-NON instructions and federal Form 2210.** The
  research read them to settle questions; nothing here computes from them.
- **City of Wilmington pages.** wilmingtonde.gov returned HTTP 403 to the
  research. Nothing here computes the city's taxes.
