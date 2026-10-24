# Where these files came from

The Vermont figures in this package are checked against these files. Seven
are sections of 32 V.S.A. from the Legislature's site. The eighth is the
research file, which stands in for the Department of Taxes' documents because
tax.vermont.gov would not serve them. Some checks could not be kept; they are
listed under "The Department's documents were blocked".

The statute pages were retrieved 2026-10-20 with curl 8.5.0 and reduced to
text with the Python 3.12.3 script below. The Legislature's pages say "The
Statutes below now include the actions of the 2026 session of the General
Assembly", which each statute extract keeps on its first line.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `vsa-32-5851.txt` | [32 V.S.A. § 5851](https://legislature.vermont.gov/statutes/section/32/151/05851), Definitions | `6d7098b3f4ff0da32b76a13028dc6a7629bd40007ba3f2c1c7ff70666450e2f8` |
| `vsa-32-5852.txt` | [32 V.S.A. § 5852](https://legislature.vermont.gov/statutes/section/32/151/05852), Payment of estimated income tax | `60448030fa7c30973e00d7cd79a9956d5449cd698bc0d91e4d2884b8bbf15452` |
| `vsa-32-5857.txt` | [32 V.S.A. § 5857](https://legislature.vermont.gov/statutes/section/32/151/05857), Filing dates | `846b2293cb35db6efb0babf5702345efb90611170f789130cb83cd6cf7e8d5a8` |
| `vsa-32-5859.txt` | [32 V.S.A. § 5859](https://legislature.vermont.gov/statutes/section/32/151/05859), Assessment date, penalties, interest | `78954a37a7c2ef13088d95893897a1e201123735c20a4bb340f915913409aa4e` |
| `vsa-32-3108.txt` | [32 V.S.A. § 3108](https://legislature.vermont.gov/statutes/section/32/103/03108), Establishment of interest rate | `61d5103d8692a2cbfe9eb610c8b7d26f9d9419cc929931a2fa188d621249fa6e` |
| `vsa-32-3202.txt` | [32 V.S.A. § 3202](https://legislature.vermont.gov/statutes/section/32/103/03202), Interest and penalties | `f6f16425c946a1d3e58e5d0afbd7244cb0f21322b1e46724932acc7d340dae74` |
| `vsa-32-3004.txt` | [32 V.S.A. § 3004](https://legislature.vermont.gov/statutes/section/32/101/03004), Sunday; time extended | `a6a79531fd8e0de1ee272147ef1d4a059eb9173e649efd08d515e478e14b593d` |
| `research-vt.md` | `research/estimated-payments/vt.md` at commit `31d590e18`, copied unchanged. **Not a primary source**: it stands in for the Department's documents, which could not be fetched | `ee766351705b30f36dfeb25ec2a4e2d55401ce04547be9cc098420c3ab2cee1d` |

A statute page's hash records what was fetched. The page carries navigation
that can change without the text changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
L=https://legislature.vermont.gov/statutes/section/32
curl -L -o 5851.html $L/151/05851
curl -L -o 5852.html $L/151/05852
curl -L -o 5857.html $L/151/05857
curl -L -o 5859.html $L/151/05859
curl -L -o 3108.html $L/103/03108
curl -L -o 3202.html $L/103/03202
curl -L -o 3004.html $L/101/03004

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>|<!--.*?-->", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div|/dt|/dd)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0\r]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY

# A statute from its heading to the site's footer, after the session note.
statute() {
  python3 html2text.py "$1" > full.txt
  grep -m1 '^The Statutes below now include' full.txt > "$3"
  awk -v h="$2" 'index($0,h)==1{f=1} f && /^Statutes$/{exit} f' full.txt >> "$3"
}
statute 5851.html "§ 5851." vsa-32-5851.txt
statute 5852.html "§ 5852." vsa-32-5852.txt
statute 5857.html "§ 5857." vsa-32-5857.txt
statute 5859.html "§ 5859." vsa-32-5859.txt
statute 3108.html "§ 3108." vsa-32-3108.txt
statute 3202.html "§ 3202." vsa-32-3202.txt
statute 3004.html "§ 3004." vsa-32-3004.txt

cp ../../research/estimated-payments/vt.md research-vt.md
```

## The Department's documents were blocked

Every tax.vermont.gov address answered a plain `curl -L -o` with HTTP 403 and
a CloudFront page reading "Request blocked." on 2026-10-20:

- `https://tax.vermont.gov/sites/tax/files/documents/IN-114-Instr-2026.pdf`,
  the 2026 Form IN-114 instructions (Rev. 10/25);
- `https://tax.vermont.gov/sites/tax/files/documents/IN-114-2026.pdf`, the
  2026 Form IN-114 voucher (Rev. 10/25);
- `https://tax.vermont.gov/sites/tax/files/documents/IN-152-2025.pdf`,
  Worksheet IN-152 for 2025 (Rev. 10/25);
- `https://tax.vermont.gov/sites/tax/files/documents/IN-152A-2025.pdf`,
  Worksheet IN-152A for 2025 (Rev. 10/25);
- `https://tax.vermont.gov/sites/tax/files/documents/IN-111-Instr-2025.pdf`,
  the 2025 Form IN-111 instructions;
- `https://tax.vermont.gov/sites/tax/files/documents/2026%20Interest%20Rate%20Memo.pdf`,
  the 2026 interest rate memo of November 4, 2025;
- `https://tax.vermont.gov/data-and-statistics/rates-and-charts/interest`,
  the Interest Rates page;
- `https://tax.vermont.gov/individuals/estimated-income-tax`, the Estimated
  Income Tax page;
- `https://tax.vermont.gov/individuals/estimated-income-taxes/interest-and-penalty`,
  the Calculate Interest and Penalties page;
- `https://tax.vermont.gov/node/10121`, the State Holiday Closures page;
- `https://tax.vermont.gov/business/file-and-pay/timely`, the Timely Filing
  and Payments page.

These figures are checked against the quote the research file's verification
read in the document itself, through `research-vt.md`: the revision dates of
IN-114's instructions and IN-152, the 90% and 100% options (voucher, IN-152
lines 1 to 3), the $500 threshold and what it is measured on (IN-111 line 31,
the Calculate Interest and Penalties page), the IN-114 instructions' and
voucher's other $500 wordings, the four equal quarters and 25% a quarter
(IN-114 instructions, IN-152 line 4), withholding at 25% a quarter (IN-152
line 5a), the two-thirds farming or fishing exemption, annualising only for
federal annualisers, the IN-152A ratio (line 6), the 7.75% rate for 2026
(Interest Rates page), part months counted as whole (IN-152 line 12), and the
holiday and weekend rules (holiday and Timely Filing pages). That checks this
package against the research, not against the Department.

The research has no quote for these, so their checks were dropped:

- **The due dates as IN-114, its voucher and the Estimated Income Tax page
  print them.** The dates are still checked against § 5852(a).
- **The 90% and 100% sentences in the IN-114 instructions and on the
  Estimated Income Tax page.** The figures are still checked against the
  voucher's quote and § 5851(2).
- **IN-152 line 1's definition of the tax**, IN-111 line 20 minus line 26c.
- **IN-152 line 5b**, payments made by each due date, and **line 5c**, a
  quarter's excess going to the next quarter.
- **IN-152 line 7**, the April 15 stop.
- **IN-152 lines 8a and 8b**, the split of days at January 1 and the fourth
  quarter's days from January 15.
- **IN-152 lines 9a and 9b**, the 365-day year and the 8.50% for 2025. The
  7.75% for 2026 is still checked through the Interest Rates page's quote.
- **IN-152 line 13**, 1% a month. It is still checked against § 3202(b)(2).
- **IN-152A lines 3 and 4**, which take federal Form 2210 line 9 and line 27.
- **The IN-111 instructions' Farmer/Fisherman paragraph**, "in either the
  current or proceeding tax year". The prior-year half of the exemption is
  not checked against any text.
- **The memo's January 1, 2026 effective date.** § 3108 still sets January 1.
- **Every holiday in the calendar**, including the Juneteenth and Thanksgiving
  rows, and the Timely Filing page's holiday sentence. The research gives no
  holiday dates. The rule that a holiday moves a date is still checked.
- **The four examples on the Calculate Interest and Penalties page.**
  `TestEstimatedDepartmentExamples` keeps their arithmetic, but their figures
  are no longer checked against the page's text.

Somebody with access should fetch these eleven documents, check the quotes,
and replace `research-vt.md` with extracts. The dropped checks should come
back with them.

## The Department's examples are the strongest check

IN-152 and IN-152A carry no worked example. The Calculate Interest and
Penalties page gives four short cases with last year's tax, this year's tax,
withholding and payments. Three state their answer, and
`TestEstimatedDepartmentExamples` reproduces them. The fourth stops before its
conclusion; under the $500 rule it owes nothing, and the test pins that as this
build's answer, not the Department's. The page is not checked in, so the
cases' figures are not checked against its text.

## Things in these documents that are wrong or out of date

- **The holiday page's 2026 Thanksgiving row.** It printed "Thursday, November
  28" and "Friday, November 29". In 2026 those dates are a Saturday and a
  Sunday; Thanksgiving is November 26. The calendar leaves both out. No due
  date is near them.
- **The interest rate on the Calculate Interest and Penalties page.** It says
  "Interest of 0.5% per month". The 2026 memo, the Interest Rates page and
  IN-152 lines 9a and 9b give an annual rate, 7.75% for 2026, which is about
  0.65% a month. 32 V.S.A. § 3108 sets an annual rate, so the memo is used.
- **IN-152's lines 1 to 3 print ".00"**, as whole-dollar fields. The worksheet
  gives no rounding rule, and this build keeps cents throughout. Interest is
  rounded to the cent on each of lines 9a and 9b.

## What is not here

- **The 2026 IN-152 and IN-152A, and the 2026 IN-111 instructions.** Not
  found as published when this package was written. The 2025 worksheets'
  arithmetic is used with the 2026 dates from IN-114 and the 2026 rate from
  the memo.
- **The rate from January 1, 2027.** § 3108 sets it by December 15, 2026, and
  it is not published. The penalty reports the gap in `Required`.
- **26 U.S.C. § 6654(i)(2)**, the federal farmer and fisherman definition the
  IN-111 instructions refer to. It is not checked in.
