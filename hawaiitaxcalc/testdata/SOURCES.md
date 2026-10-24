# Where these files came from

Every Hawaii figure in this package is checked against a document the Hawaii
Department of Taxation or the Hawaii State Legislature published, and one
federal text that Hawaii's statute adopts. These files are text extracted from
them. This records which document, and how, so that anybody can redo it and
diff the result.

Retrieved 2026-10-20 with curl 8.5.0, with no special headers. PDFs were
extracted with `pdftotext` from poppler 24.02.0, and HTML pages reduced to
text with the Python 3.12.3 script below. `T` is
`https://files.hawaii.gov/tax` and `H` is
`https://data.capitol.hawaii.gov/hrscurrent`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `tf2019-3.txt` | [Tax Facts 2019-3, Estimated Income Tax for Individuals, revised February 2026](https://files.hawaii.gov/tax/legal/taxfacts/tf2019-3.pdf) (`T/legal/taxfacts/tf2019-3.pdf`), whole document, reading order | `dd208f187d84d152345079b8459a6eb4208a4000c60243c72124efb76cefded2` |
| `n210i-2025.txt` | [Instructions for Form N-210 (Rev. 2025)](https://files.hawaii.gov/tax/forms/2025/n210ins.pdf) (`T/forms/2025/n210ins.pdf`), whole document, reading order | `8ce44de717825ec2c02e8316d97fa1302e412bd62c3a7b83cfffc15a78e81dd8` |
| `n210-2025.txt` | [Form N-210 (Rev. 2025)](https://files.hawaii.gov/tax/forms/2025/n210_i.pdf) (`T/forms/2025/n210_i.pdf`), whole form, page layout | `61511c4353a2024caea1fcca29931b39ea6dcc5b3bf29c557399b64d6a6c03cc` |
| `n11i-2025-estimated-tax.txt` | [Form N-11 Instructions (Rev. 2025)](https://files.hawaii.gov/tax/forms/2025/n11ins.pdf) (`T/forms/2025/n11ins.pdf`), the section Estimated Tax Payments, which prints the 2026 due dates | `7160595e466b376ba9c14da1540d21532be9e05d48d01d877f5d953c85e4e4d4` |
| `har-18-235-97.txt` | [HAR chapter 18-235, unofficial compilation as of 12/31/2025](https://files.hawaii.gov/tax/legal/har/har_235.pdf) (`T/legal/har/har_235.pdf`), § 18-235-97 | `017054c120f20c6049c68d4cf74ef7610a2deaed7a51e56c254e88d6a3c19f67` |
| `ann26-06.txt` | [Department of Taxation Announcement No. 2026-06, Tax Law Changes from the 2026 Regular Legislative Session (July 31, 2026)](https://files.hawaii.gov/tax/news/announce/ann26-06.pdf), whole document | `52465df7ce0c31ff6e06d902e9a4914d8829e1afad46abc24b1cedc2491c47df` |
| `hrs-235-97.txt` | [HRS § 235-97](https://data.capitol.hawaii.gov/hrscurrent/Vol04_Ch0201-0257/HRS0235/HRS_0235-0097.htm) (`H/Vol04_Ch0201-0257/HRS0235/HRS_0235-0097.htm`) | `f9c5e57b2cad96bf940f42ec26fb2f4ce5208a0d14ef7957f45865fb9a5ee010` (the HTML) |
| `hrs-231-21.txt` | [HRS § 231-21](https://data.capitol.hawaii.gov/hrscurrent/Vol04_Ch0201-0257/HRS0231/HRS_0231-0021.htm) (`H/Vol04_Ch0201-0257/HRS0231/HRS_0231-0021.htm`) | `246f10ff4d8775b28811333b5ef637d8794c1d92402e39d8928b948c0b6b7acd` (the HTML) |
| `hrs-8-1.txt` | [HRS § 8-1](https://data.capitol.hawaii.gov/hrscurrent/Vol01_Ch0001-0042F/HRS0008/HRS_0008-0001.htm) (`H/Vol01_Ch0001-0042F/HRS0008/HRS_0008-0001.htm`) | `35200fdd35e141184540e15f4f1b9177a1ae6b4cc55a8ce30b7402daf3d800bf` (the HTML) |
| `usc-26-6654-e-h.txt` | [26 U.S.C. § 6654, United States Code, 2024 Edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleF-chap68-subchapA-partI-sec6654.htm), subsections (e) to (h) | `4d7fcd8ee19a8ba3f5502404a47a9f839378abf0d9a4c01d0adcc00483d480ff` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

www.capitol.hawaii.gov returns 403 to curl, as the research file says. The
same statute pages load from data.capitol.hawaii.gov, which is the
legislature's own host.

## Reproducing them

```bash
T=https://files.hawaii.gov/tax
H=https://data.capitol.hawaii.gov/hrscurrent
curl -o tf2019-3.pdf  $T/legal/taxfacts/tf2019-3.pdf
curl -o n210ins.pdf   $T/forms/2025/n210ins.pdf
curl -o n210_i.pdf    $T/forms/2025/n210_i.pdf
curl -o n11ins.pdf    $T/forms/2025/n11ins.pdf
curl -o har_235.pdf   $T/legal/har/har_235.pdf
curl -o ann26-06.pdf  $T/news/announce/ann26-06.pdf
curl -o HRS_0235-0097.htm $H/Vol04_Ch0201-0257/HRS0235/HRS_0235-0097.htm
curl -o HRS_0231-0021.htm $H/Vol04_Ch0201-0257/HRS0231/HRS_0231-0021.htm
curl -o HRS_0008-0001.htm $H/Vol01_Ch0001-0042F/HRS0008/HRS_0008-0001.htm
curl -o usc-6654.htm https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleF-chap68-subchapA-partI-sec6654.htm

pdftotext tf2019-3.pdf tf2019-3.txt
pdftotext n210ins.pdf n210i-2025.txt
pdftotext -layout n210_i.pdf n210-2025.txt
pdftotext n11ins.pdf n11ins.txt
awk '/^Estimated Tax Payments$/{f=1} /^Electronic Funds Transfer$/{if(f)exit} f' n11ins.txt \
  > n11i-2025-estimated-tax.txt
pdftotext har_235.pdf har.txt
awk '/^Estimates; tax payments; returns\. \(a\) Definitions/{f=1} f; /\(Imp: HRS §235-97\)/{if(f)exit}' har.txt \
  > har-18-235-97.txt
pdftotext ann26-06.pdf ann26-06.txt

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8-sig", errors="replace").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py HRS_0235-0097.htm > hrs-235-97.txt
python3 html2text.py HRS_0231-0021.htm > hrs-231-21.txt
python3 html2text.py HRS_0008-0001.htm > hrs-8-1.txt
python3 html2text.py usc-6654.htm \
  | awk '/^\(e\) Exceptions$/{f=1} /^\(i\) Special rules for farmers and fishermen$/{f=0} f' \
  > usc-26-6654-e-h.txt
```

The Tax Facts and the N-210 instructions are set in columns, so they are
extracted in reading order; Form N-210 is a grid of lines and columns, so it
is extracted with its layout. The N-11 instructions run to 52 pages and the
HAR compilation to 1,689, so only the estimated tax section of the one and
§ 18-235-97 of the other are kept, bounded by their own headings. Of § 6654,
only subsections (e) to (h) are kept: the no-liability exception and the
waivers the divergences and the explanation cite, and the January 31 rule.

## What is the strong check

Hawaii publishes no worked penalty with a stated answer. Two printed figures
come close, and both are tests. Form N-210 line 19 prints the months for a
shortfall left unpaid to April 20 of the next year: 12, 10, 7 and 3. The
instructions work Mr. Aloha's example, a $1,000 April shortfall paid on June
10, to two months. Everything else is hand-written cases, worked by hand in
the test comments.

## What is not here

- **The 2026 Form N-210 and its instructions.** Not published on 2026-10-20.
  The 2025 form's arithmetic is used with the 2026 dates. Its threshold,
  percentages and rate are fixed in HRS § 235-97, which was last amended in
  2003; Announcement No. 2026-06 lists no 2026 change to it.
- **Printed 2026 dates moved for the weekend.** No department document
  prints June 22 or September 21, 2026. The N-11 instructions print June 20,
  2026 and September 20, 2026, and Tax Facts 2019-3, the N-210 instructions
  and HRS § 231-21 move a date on a weekend to the next business day. The
  test checks the computed dates against the printed ones moved by that rule,
  and checks them against the § 8-1 holidays.
- **26 U.S.C. § 6654 as of December 31, 2025.** HRS § 235-97(f)(4) adopts it
  as of the date in § 235-2.3(a), which Act 35 (2026) set at December 31,
  2025. The 2024 Edition of the Code is the latest on govinfo.gov; it does not
  show whether 2025 legislation touched the adopted subsections. Hawaii's own
  percentages, threshold, dates and rate are in § 235-97 itself and do not
  depend on it.
- **Form N-200V**, the payment voucher. Nothing here computes from it.
