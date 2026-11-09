// The jurisdictions the home page offers, and what the site knows of each.
//
// Only the jurisdictions in `documented` have pages built from the rules.
// The others show how they tax wages and whether taxcalcs prepares a return
// for them yet.

export type TaxKind = 'graduated' | 'flat' | 'none';

export interface Jurisdiction {
  code: string;
  name: string;
  /** How it taxes wages in 2025. */
  kind: TaxKind;
  /** Its resident return, if it has an income tax. */
  form?: string;
  /** Its place on the tile map: column, row. */
  at: [number, number];
}

export const kindLabels: Record<TaxKind, string> = {
  graduated: 'Graduated rates',
  flat: 'Flat rate',
  none: 'No tax on wages',
};

// prettier-ignore
export const states: Jurisdiction[] = [
  { code: 'AK', name: 'Alaska', kind: 'none', at: [0, 0] },
  { code: 'ME', name: 'Maine', kind: 'graduated', form: 'Form 1040ME', at: [10, 0] },
  { code: 'VT', name: 'Vermont', kind: 'graduated', form: 'Form IN-111', at: [9, 1] },
  { code: 'NH', name: 'New Hampshire', kind: 'none', at: [10, 1] },
  { code: 'WA', name: 'Washington', kind: 'none', at: [0, 2] },
  { code: 'ID', name: 'Idaho', kind: 'flat', form: 'Form 40', at: [1, 2] },
  { code: 'MT', name: 'Montana', kind: 'graduated', form: 'Form 2', at: [2, 2] },
  { code: 'ND', name: 'North Dakota', kind: 'graduated', form: 'Form ND-1', at: [3, 2] },
  { code: 'MN', name: 'Minnesota', kind: 'graduated', form: 'Form M1', at: [4, 2] },
  { code: 'IL', name: 'Illinois', kind: 'flat', form: 'Form IL-1040', at: [5, 2] },
  { code: 'WI', name: 'Wisconsin', kind: 'graduated', form: 'Form 1', at: [6, 2] },
  { code: 'MI', name: 'Michigan', kind: 'flat', form: 'Form MI-1040', at: [7, 2] },
  { code: 'NY', name: 'New York', kind: 'graduated', form: 'Form IT-201', at: [8, 2] },
  { code: 'RI', name: 'Rhode Island', kind: 'graduated', form: 'Form RI-1040', at: [9, 2] },
  { code: 'MA', name: 'Massachusetts', kind: 'flat', form: 'Form 1', at: [10, 2] },
  { code: 'OR', name: 'Oregon', kind: 'graduated', form: 'Form OR-40', at: [0, 3] },
  { code: 'NV', name: 'Nevada', kind: 'none', at: [1, 3] },
  { code: 'WY', name: 'Wyoming', kind: 'none', at: [2, 3] },
  { code: 'SD', name: 'South Dakota', kind: 'none', at: [3, 3] },
  { code: 'IA', name: 'Iowa', kind: 'flat', form: 'Form IA 1040', at: [4, 3] },
  { code: 'IN', name: 'Indiana', kind: 'flat', form: 'Form IT-40', at: [5, 3] },
  { code: 'OH', name: 'Ohio', kind: 'graduated', form: 'Form IT 1040', at: [6, 3] },
  { code: 'PA', name: 'Pennsylvania', kind: 'flat', form: 'Form PA-40', at: [7, 3] },
  { code: 'NJ', name: 'New Jersey', kind: 'graduated', form: 'Form NJ-1040', at: [8, 3] },
  { code: 'CT', name: 'Connecticut', kind: 'graduated', form: 'Form CT-1040', at: [9, 3] },
  { code: 'CA', name: 'California', kind: 'graduated', form: 'Form 540', at: [0, 4] },
  { code: 'UT', name: 'Utah', kind: 'flat', form: 'Form TC-40', at: [1, 4] },
  { code: 'CO', name: 'Colorado', kind: 'flat', form: 'Form DR 0104', at: [2, 4] },
  { code: 'NE', name: 'Nebraska', kind: 'graduated', form: 'Form 1040N', at: [3, 4] },
  { code: 'MO', name: 'Missouri', kind: 'graduated', form: 'Form MO-1040', at: [4, 4] },
  { code: 'KY', name: 'Kentucky', kind: 'flat', form: 'Form 740', at: [5, 4] },
  { code: 'WV', name: 'West Virginia', kind: 'graduated', form: 'Form IT-140', at: [6, 4] },
  { code: 'VA', name: 'Virginia', kind: 'graduated', form: 'Form 760', at: [7, 4] },
  { code: 'MD', name: 'Maryland', kind: 'graduated', form: 'Form 502', at: [8, 4] },
  { code: 'DE', name: 'Delaware', kind: 'graduated', form: 'Form 200-01', at: [9, 4] },
  { code: 'AZ', name: 'Arizona', kind: 'flat', form: 'Form 140', at: [1, 5] },
  { code: 'NM', name: 'New Mexico', kind: 'graduated', form: 'Form PIT-1', at: [2, 5] },
  { code: 'KS', name: 'Kansas', kind: 'graduated', form: 'Form K-40', at: [3, 5] },
  { code: 'AR', name: 'Arkansas', kind: 'graduated', form: 'Form AR1000F', at: [4, 5] },
  { code: 'TN', name: 'Tennessee', kind: 'none', at: [5, 5] },
  { code: 'NC', name: 'North Carolina', kind: 'flat', form: 'Form D-400', at: [6, 5] },
  { code: 'SC', name: 'South Carolina', kind: 'graduated', form: 'Form SC1040', at: [7, 5] },
  { code: 'DC', name: 'District of Columbia', kind: 'graduated', form: 'Form D-40', at: [8, 5] },
  { code: 'OK', name: 'Oklahoma', kind: 'graduated', form: 'Form 511', at: [3, 6] },
  { code: 'LA', name: 'Louisiana', kind: 'flat', form: 'Form IT-540', at: [4, 6] },
  { code: 'MS', name: 'Mississippi', kind: 'flat', form: 'Form 80-105', at: [5, 6] },
  { code: 'AL', name: 'Alabama', kind: 'graduated', form: 'Form 40', at: [6, 6] },
  { code: 'GA', name: 'Georgia', kind: 'flat', form: 'Form 500', at: [7, 6] },
  { code: 'HI', name: 'Hawaii', kind: 'graduated', form: 'Form N-11', at: [0, 7] },
  { code: 'TX', name: 'Texas', kind: 'none', at: [3, 7] },
  { code: 'FL', name: 'Florida', kind: 'none', at: [8, 7] },
];

export const byCode = new Map(states.map((s) => [s.code.toLowerCase(), s]));

/** What the site knows of a jurisdiction whose rules it documents. */
export interface Documented {
  /** How it taxes income, in a few sentences. */
  summary: string;
  /** Who the documented return is for, and what is not yet documented. */
  scope: string;
  /** The main form's rule, which the page's tree is built from. */
  form: string;
  /** The form source whose lines measure coverage. */
  formSource: string;
  /** The forms the rules compute, with the rule each is documented by. */
  forms: { title: string; subtitle: string; rule: string; feeds?: string; walkthrough?: string }[];
  /** The figures shown at a glance: a label, and figure keys whose values are joined. */
  glance: { label: string; keys: string[]; suffix?: string }[];
}

export const documented: Record<string, Documented> = {
  ca: {
    summary:
      "California taxes a full-year resident's income at graduated rates from 1% to 12.3%, plus 1% on taxable income over $1 million. The return starts from federal AGI, then applies California's own adjustments, standard deduction and credits.",
    scope: 'Covers full-year residents. Part-year and non-resident returns (Form 540NR) are not yet documented.',
    form: 'ca-form-540',
    formSource: 'ca-ftb-form-540-2025',
    forms: [
      {
        title: 'Form 540',
        subtitle: 'California Resident Income Tax Return',
        rule: 'ca-form-540',
        walkthrough: '',
      },
      {
        title: 'Schedule CA (540)',
        subtitle: 'California adjustments to federal income and deductions',
        rule: 'ca-schedule-ca',
        feeds: 'lines 14 and 16',
        walkthrough: 'line-14',
      },
      {
        title: 'Form FTB 3853',
        subtitle: 'Health coverage and the shared responsibility penalty',
        rule: 'ca-shared-responsibility-penalty',
        feeds: 'line 92',
        walkthrough: 'line-92',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['CaliforniaYear.standard_deduction_single', 'CaliforniaYear.standard_deduction_joint'] },
      { label: 'Exemption credit', keys: ['CaliforniaYear.personal_exemption_credit'], suffix: ' each' },
      { label: 'Credits reduced above', keys: ['CaliforniaYear.phaseout_agi_single'], suffix: ' AGI' },
      { label: "Renter's credit", keys: ['CaliforniaYear.renters_credit_single', 'CaliforniaYear.renters_credit_joint'] },
      { label: 'Penalty, per adult', keys: ['CaliforniaYear.isr_adult'] },
    ],
  },
};

/** Where a jurisdiction's page is. */
export function jurisdictionPath(code: string, year: number): string[] {
  return ['/', String(year), code.toLowerCase()];
}
