# Where these files came from

Every Maryland figure in this package is checked against a document the
Comptroller of Maryland or the General Assembly published. These files are text
extracted from those documents. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. `C` is `https://www.marylandcomptroller.gov/content/dam/mdcomp/tax`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `pvw-2026.txt` | [Payment Voucher Worksheet for Estimated Tax and Extension Payments (PVW)](https://www.marylandcomptroller.gov/content/dam/mdcomp/tax/forms/worksheets/2025-PV-Worksheet.pdf) (`C/forms/worksheets/2025-PV-Worksheet.pdf`), from the start to the Extension Worksheet Instructions, reading order. The file name says 2025; the worksheet is for tax year 2026 | `df30c2cbf02a0cf38caf6167284dab9e316e5cfca96ac2a6a182b3c74246807e` |
| `502up-2025.txt` | [Form 502UP, Underpayment of Estimated Income Tax by Individuals, 2025 (COM/RAD 017 09/25)](https://www.marylandcomptroller.gov/content/dam/mdcomp/tax/forms/2025/502up.pdf) (`C/forms/2025/502up.pdf`), form and instructions, reading order | `4f2b10661a071fe8aa1d4329da5d08a20c00554c40f7ae6fbc9fa4a0694e8510` |
| `502up-2025-columns.txt` | the same PDF, the column headings to line 16, page layout | same |
| `booklet-2025.txt` | [2025 Maryland State and Local Tax Forms and Instructions](https://www.marylandcomptroller.gov/content/dam/mdcomp/tax/instructions/2025/resident-booklet.pdf) (`C/instructions/2025/resident-booklet.pdf`): the State Income Tax Rates note, the rounding paragraph, Underpayment of Estimated Tax to Total Interest and Penalty, and the code-number paragraph, reading order | `17bc9f77e055c0570daee8840921e635b1b0952a39d54f28e33516699b9b6ba5` |
| `md-register-53-18.txt` | [Maryland Register, Volume 53, Issue 18 (September 4, 2026)](https://dsd.maryland.gov/MDRIssues/5318/Assembled.aspx): the issue header and the Comptroller's Notice of Interest Rate on Refunds and Moneys Owed to the State | `f87f721d355896fce126ef0f5003b86704912228e117eed7f0178687ba79eed6` (the HTML) |
| `tax-general.txt` | Md. Code, Tax-General §§ [1-201](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=1-201&enactments=false), [10-815](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=10-815&enactments=false), [10-820](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=10-820&enactments=false), [10-902](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=10-902&enactments=false), [13-602](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=13-602&enactments=false), [13-604](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=13-604&enactments=false), [13-606](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=13-606&enactments=false), [13-702](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=13-702&enactments=false) and [13-714](https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=13-714&enactments=false), the statute text of each page, in that order | 1-201 `f264796057954b898c8331baf60996acac2fc054d1d59235da42f509b52f0b9b`; 10-815 `568c706e660e4b48c41acaa54119bcd19ff9c52792c0c3012428cc3fd16ad61f`; 10-820 `c76a4da7856de54da4e8d583baa28d9c88bc9aa63bc50132151f133f7feb461f`; 10-902 `735a876daddfa7e41a19ab77037b63984135967cfb526ce7012fded045f49972`; 13-602 `3502b3f983f12e31d326c67a765a36e21de34a5d99ec4f0afe0f844b70ec783b`; 13-604 `854cbba8978812b667f6b7e44d2c7afdf0dbe9c8def6d44373058bc63e405125`; 13-606 `ec4f9b1737862c096f388c9b210180a97705de72fb96b4d7a5ee7360ed9ab49b`; 13-702 `3f8249f3eba691d2397d31c8648a96a639ce5fd5c15f431cec7fee8dff33465a`; 13-714 `9149a2c40c7dbaa7f9b21634520df4a6b77082a646449474f1d1716cfe75bb77` (the HTML) |

An HTML page's hash records what was fetched. The pages carry navigation that
changes without the content changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
C=https://www.marylandcomptroller.gov/content/dam/mdcomp/tax
curl -o pvw.pdf     "$C/forms/worksheets/2025-PV-Worksheet.pdf"
curl -o 502up.pdf   "$C/forms/2025/502up.pdf"
curl -o booklet.pdf "$C/instructions/2025/resident-booklet.pdf"
curl -o mdr.html    https://dsd.maryland.gov/MDRIssues/5318/Assembled.aspx
for s in 1-201 10-815 10-820 10-902 13-602 13-604 13-606 13-702 13-714; do
  curl -o "gtg-$s.html" "https://mgaleg.maryland.gov/mgawebsite/Laws/StatuteText?article=gtg&section=$s&enactments=false"
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

pdftotext pvw.pdf pvw.txt
awk 'NR==1,/^EXTENSION WORKSHEET INSTRUCTIONS$/' pvw.txt > pvw-2026.txt

pdftotext 502up.pdf 502up-2025.txt
pdftotext -layout 502up.pdf 502up.L.txt
awk '/DUE DATES OF INSTALLMENTS/{f=1} f; /^ *16\. Interest factor/{if(f)exit}' 502up.L.txt \
  > 502up-2025-columns.txt

pdftotext booklet.pdf booklet.txt
{ awk '/^State Income Tax Rates: /{f=1} f; /See page 32 for the updated rate information\./{if(f)exit}' booklet.txt
  awk '/^You may round off all cents to the nearest whole dollar/{f=1} f; /rounded to the nearest cent\.$/{if(f)exit}' booklet.txt
  awk '/^UNDERPAYMENT OF ESTIMATED TAX$/{f=1} f; /^TOTAL INTEREST AND PENALTY$/{if(f)exit}' booklet.txt
  awk '/^If special circumstances apply, you may not owe interest/{f=1} f; /entered code 301\.$/{if(f)exit}' booklet.txt
} > booklet-2025.txt

python3 html2text.py mdr.html > mdr.txt
{ awk '/^Maryland Register$/{f=1} f; /^Volume 53/{if(f)exit}' mdr.txt
  awk '/^Notice of Interest$/{f=1} f; /^\[26-18-14\]$/{if(f)exit}' mdr.txt
} > md-register-53-18.txt

for s in 1-201 10-815 10-820 10-902 13-602 13-604 13-606 13-702 13-714; do
  python3 html2text.py "gtg-$s.html" |
    awk '/^Article - Tax - General$/{f=1;next} f&&/^PreviousNext$/{n++; if(n==2)exit; next} f'
done > tax-general.txt
```

The worksheet, the 502UP instructions and the booklet are set in two
columns, so they are extracted in reading order. Form 502UP's columns lose
their rows in reading order, so the due dates and factors are extracted again
with their layout. The booklet is 2.8 MB, so only its estimated-tax passages
are kept, bounded by its own headings. Two-column reading order interleaves
some unrelated text (direct deposit) into the booklet extract; the tests match
phrases, so it does no harm.

## What is not here

- **The 2026 Form 502UP and the 2026 booklet.** Not published:
  `C/forms/2026/502up.pdf` and `C/instructions/2026/resident-booklet.pdf`
  returned the site's 404 page on 2026-10-20. The 2025 form's arithmetic is
  used, and its printed interest factors are not, because they are worked out
  from 2025's rates. Without a 2026 factor the interest is charged by the
  month as Tax-General § 13-604(a) says, and the penalty reports that in
  `Required`.
- **COMAR 03.04.01.02.** It agrees with the statute on every rule used here.
  The text is licensed CC BY-NC-SA and its site asks not to be scraped, so it
  is not checked in; `research/estimated-payments/md.md` quotes it.
- **26 U.S.C. § 6654(e)(1).** The statute's $500 is half of its $1,000; the
  worksheet and Form 502UP print $500 directly, and those are checked.
- **Form PV (2026).** The voucher itself carries no figure the rules use.

Nothing was blocked: every document above downloaded with plain `curl`.
