# Where these files came from

The finding in this package rests on what the Florida Department of Revenue
published. These files are text extracted from those documents. This records
which document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs are extracted with `pdftotext` from
poppler 24.02.0.

| File | Source | sha256 of the download |
|---|---|---|
| `dor-fl-gt800025.txt` | [Florida Department of Revenue, Tax Information for New Residents, GT-800025 (R. 08/25)](https://floridarevenue.com/Forms_library/current/brochure/gt800025.pdf) | `9b0c796b958dc3569a7f4c6f00c7b6b8562e4e47eedca83b52d426ef5ac782f6` |

## Reproducing them

```bash
curl -L -o gt800025.pdf https://floridarevenue.com/Forms_library/current/brochure/gt800025.pdf
pdftotext gt800025.pdf dor-fl-gt800025.txt
```
