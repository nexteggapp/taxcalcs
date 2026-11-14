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
  { code: 'DE', name: 'Delaware', kind: 'graduated', form: 'Form PIT-RES', at: [9, 4] },
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
  /** The rate schedule whose top band gives the top rate, if graduated, and a surtax rate added to it. */
  schedule?: string;
  surtax?: string;
}

export const documented: Record<string, Documented> = {
  us: {
    summary:
      'The federal income tax is charged at graduated rates from 10% to 37% on taxable income: income less adjustments, less the standard or itemized deductions and the newer deductions of Schedule 1-A. Qualified dividends and long-term capital gains are taxed at 0%, 15% or 20%. Credits come off the tax, and the refundable ones can make a refund of more than was withheld.',
    scope:
      'Covers Form 1040 for citizens and residents, with the schedules and worksheets it draws on. Form 1040-NR, a dual-status return, and the lines listed under Left out are not computed.',
    form: 'fed-form-1040',
    formSource: 'us-irs-form-1040-2025',
    forms: [
      {
        title: 'Form 1040',
        subtitle: 'U.S. Individual Income Tax Return',
        rule: 'fed-form-1040',
        walkthrough: '',
      },
      {
        title: 'Social Security Benefits Worksheet',
        subtitle: 'How much of the benefits is taxable',
        rule: 'compute-ss-benefits-worksheet',
        feeds: 'lines 6a and 6b',
        walkthrough: 'line-6b',
      },
      {
        title: 'Standard Deduction Worksheet',
        subtitle: 'For dependents, age and blindness',
        rule: 'compute-standard-deduction-worksheet',
        feeds: 'line 12e',
        walkthrough: 'line-12e',
      },
      {
        title: 'Qualified Dividends and Capital Gain Tax Worksheet',
        subtitle: 'The tax at 0%, 15% and 20%',
        rule: 'compute-qdcgt-worksheet',
        feeds: 'line 16',
        walkthrough: 'line-16',
      },
      {
        title: 'Schedule 2',
        subtitle: 'Additional taxes',
        rule: 'compute-schedule-2',
        feeds: 'lines 17 and 23',
      },
      {
        title: 'Schedule 3',
        subtitle: 'Additional credits and payments',
        rule: 'compute-schedule-3',
        feeds: 'lines 20 and 31',
      },
      {
        title: 'Schedule 8812',
        subtitle: 'Child tax credit, credit for other dependents, and additional child tax credit',
        rule: 'compute-ctc-odc-actc',
        feeds: 'lines 19 and 28',
        walkthrough: 'line-19',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['FederalYear.std_ded_single', 'FederalYear.std_ded_mfj', 'FederalYear.std_ded_hoh'] },
      { label: 'Child tax credit', keys: ['FederalYear.ctc_per_child'], suffix: ' per child' },
      { label: 'Refundable up to', keys: ['FederalYear.actc_max_per_child'], suffix: ' per child' },
      { label: 'State and local tax limit', keys: ['FederalYear.salt_cap_default'] },
      { label: 'Social security wage base', keys: ['FederalYear.oasdi_wage_base'] },
    ],
    schedule: 'fed-schedule-x-2025',
  },
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
    schedule: 'ca-schedule-x-2025',
    surtax: 'CaliforniaYear.behavioral_health_rate_bp',
  },
  or: {
    summary:
      "Oregon taxes personal income at graduated rates from 4.75% to 9.9%. The return begins with federal taxable income, allows a deduction for federal tax liability up to an income-tested cap, and applies Oregon's standard deduction and personal exemption credits.",
    scope: 'Covers full-year residents filing Form OR-40. Part-year residents and nonresidents (Form OR-40-N and Form OR-40-P) are not yet documented.',
    form: 'or-form-or-40',
    formSource: 'or-dor-form-or-40-2025',
    schedule: 'or-chart-s-2025',
    forms: [
      {
        title: 'Form OR-40',
        subtitle: 'Oregon Individual Income Tax Return for Full-Year Residents',
        rule: 'or-form-or-40',
        walkthrough: '',
      },
      {
        title: 'Federal Tax Liability Subtraction',
        subtitle: 'Subtractions from income for federal tax liability',
        rule: 'or-federal-tax-liability',
        feeds: 'line 10',
        walkthrough: 'line-10',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['OregonYear.standard_deduction_single', 'OregonYear.standard_deduction_joint'] },
      { label: 'Exemption credit', keys: ['OregonYear.exemption_credit'], suffix: ' each' },
      { label: 'Credits reduced above', keys: ['OregonYear.exemption_agi_limit_single'], suffix: ' AGI' },
      { label: 'Political contribution credit', keys: ['OregonYear.political_credit_single', 'OregonYear.political_credit_joint'] },
      { label: 'College / ABLE savings credit', keys: ['OregonYear.savings_credit_single', 'OregonYear.savings_credit_joint'] },
    ],
  },
  il: {
    summary:
      "Illinois taxes net income at a flat rate of 4.95%. The return starts from federal adjusted gross income, allows standard personal exemption allowances subject to income phaseouts, and provides credits for residential property tax paid, earned income, and child tax credit.",
    scope: 'Covers full-year residents filing Form IL-1040. Nonresidents and part-year residents (Schedule NR) are not yet documented.',
    form: 'il-form-il1040',
    formSource: 'il-dor-form-il-1040-2025',
    forms: [
      {
        title: 'Form IL-1040',
        subtitle: 'Individual Income Tax Return',
        rule: 'il-form-il1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['IllinoisYear.flat_rate_bp'] },
      { label: 'Basic exemption allowance', keys: ['IllinoisYear.basic_exemption'] },
      { label: 'Exemption allowance phased out above', keys: ['IllinoisYear.exemption_threshold_single'], suffix: ' AGI' },
      { label: 'Property tax credit rate', keys: ['IllinoisYear.property_tax_credit_rate_bp'] },
      { label: 'Illinois EITC rate', keys: ['IllinoisYear.eitc_rate_bp'] },
    ],
  },
  ia: {
    summary:
      "Iowa taxes Iowa taxable income at a flat rate of 3.8% for 2025. The return starts from federal taxable income, subtracts Social Security benefits and, from age 55, retirement income, and allows exemption credits, a low-income exemption, an alternate tax and the Iowa earned income tax credit.",
    scope: 'Covers full-year residents filing Form IA 1040. Nonresidents and part-year residents (IA 126) are not yet documented.',
    form: 'ia-form-ia1040',
    formSource: 'ia-idr-form-ia-1040-2025',
    forms: [
      {
        title: 'Form IA 1040',
        subtitle: 'Iowa Individual Income Tax Return',
        rule: 'ia-form-ia1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['IowaYear.flat_rate_bp'] },
      { label: 'Personal exemption credit', keys: ['IowaYear.personal_credit'] },
      { label: 'Dependent exemption credit', keys: ['IowaYear.dependent_credit'] },
      { label: 'Low-income exemption, single', keys: ['IowaYear.low_income_single'] },
      { label: 'Iowa EITC rate', keys: ['IowaYear.eitc_rate_bp'] },
    ],
  },
  in: {
    summary:
      "Indiana taxes Indiana adjusted gross income at a flat rate of 3% for 2025, and each county adds its own rate on the same income on the same form. The return starts from federal adjusted gross income, adds back and deducts the Schedule 1 and 2 items, and takes off the Schedule 3 exemptions.",
    scope: 'Covers full-year residents filing Form IT-40, with the county tax of Schedule CT-40. Part-year residents and nonresidents (Form IT-40PNR) are not yet documented.',
    form: 'in-form-it40',
    formSource: 'in-dor-form-it-40-2025',
    forms: [
      {
        title: 'Form IT-40',
        subtitle: 'Indiana Full-Year Resident Individual Income Tax Return',
        rule: 'in-form-it40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['IndianaYear.flat_rate_bp'] },
      { label: 'Personal exemption', keys: ['IndianaYear.personal_exemption'], suffix: ' each' },
      { label: 'Additional child exemption', keys: ['IndianaYear.additional_child_exemption'] },
      { label: 'Indiana EITC rate', keys: ['IndianaYear.eitc_rate_bp'] },
    ],
  },
  mi: {
    summary:
      "Michigan taxes income at a flat rate of 4.25% for 2025. The return starts from federal adjusted gross income, adjusts it on Schedule 1, takes off an exemption allowance for each filer and dependent, and refunds the property tax and earned income credits.",
    scope: 'Covers full-year residents filing Form MI-1040. Nonresidents and part-year residents (Schedule NR) and city income taxes are not yet documented.',
    form: 'mi-form-mi1040',
    formSource: 'mi-treasury-form-mi-1040-2025',
    forms: [
      {
        title: 'Form MI-1040',
        subtitle: 'Individual Income Tax Return',
        rule: 'mi-form-mi1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['MichiganYear.flat_rate_bp'] },
      { label: 'Personal exemption', keys: ['MichiganYear.personal_exemption'], suffix: ' each' },
      { label: 'Special exemption', keys: ['MichiganYear.special_exemption'] },
      { label: 'Michigan EITC rate', keys: ['MichiganYear.eitc_rate_bp'] },
    ],
  },
  pa: {
    summary:
      "Pennsylvania taxes eight classes of income at a flat rate of 3.07%. It does not start from federal adjusted gross income: compensation, interest, dividends, business income, gains, rents, estate and trust income and gambling winnings are each figured under Pennsylvania rules, and a loss in one class does not reduce another. Tax Forgiveness on Schedule SP forgives all or part of the tax for households with low eligibility income.",
    scope: 'Covers full-year residents filing Form PA-40. Compensation, interest and dividends come from the federal documents; the other classes are entered as Pennsylvania figures. Local earned income taxes are separate returns and are not covered.',
    form: 'pa-form-pa40',
    formSource: 'pa-dor-form-pa-40-2025',
    forms: [
      {
        title: 'Form PA-40',
        subtitle: 'Pennsylvania Income Tax Return',
        rule: 'pa-form-pa40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['PennsylvaniaYear.flat_rate_bp'] },
      { label: 'Tax Forgiveness, unmarried', keys: ['PennsylvaniaYear.forgiveness_base_unmarried'], suffix: ' eligibility income' },
      { label: 'Tax Forgiveness, married', keys: ['PennsylvaniaYear.forgiveness_base_married'], suffix: ' eligibility income' },
      { label: 'Added per dependent child', keys: ['PennsylvaniaYear.forgiveness_per_dependent'] },
    ],
  },
  mt: {
    summary:
      "Montana taxes ordinary income at 4.7% and 5.9%, and net long-term capital gains at 3% and 4.1%. The return starts from federal taxable income without the qualified business income deduction, subtracts $5,660 for each filer 65 or older, and refunds 10% of the federal earned income credit.",
    scope: 'Covers full-year residents filing Form 2. Part-year residents and nonresidents (Schedule II) and the Schedule III credits are not yet documented.',
    form: 'mt-form-2',
    formSource: 'mt-dor-form-2-2025',
    forms: [
      {
        title: 'Form 2',
        subtitle: 'Individual Income Tax Return',
        rule: 'mt-form-2',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Ordinary income rates', keys: ['MontanaYear.ordinary_low_rate_bp', 'MontanaYear.ordinary_high_rate_bp'] },
      { label: 'Lower bracket ends at', keys: ['MontanaYear.bracket_top_single', 'MontanaYear.bracket_top_joint'] },
      { label: 'Age 65 subtraction', keys: ['MontanaYear.age_65_subtraction'], suffix: ' each' },
      { label: 'Montana EITC rate', keys: ['MontanaYear.eitc_rate_bp'] },
    ],
  },
  al: {
    summary:
      'Alabama taxes taxable income at 2%, 4% and 5%. The return starts from wages, interest and dividends, and allows a deduction for federal income tax, a standard deduction that phases out with income, and personal and dependent exemptions.',
    scope: 'Covers full-year residents filing Form 40. Part-year residents, Schedule OC credits (taken as inputs) and local occupational taxes are not yet documented.',
    form: 'al-form-40',
    formSource: 'al-dor-form-40-booklet-2025',
    forms: [
      {
        title: 'Form 40',
        subtitle: 'Individual Income Tax Return',
        rule: 'al-form-40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Personal exemption, single', keys: ['AlabamaYear.personal_exemption_single'] },
      { label: 'Standard deduction, joint', keys: ['AlabamaYear.standard_joint'] },
      { label: 'Dependent exemption', keys: ['AlabamaYear.dependent_exemption_low'] },
    ],
  },
  oh: {
    summary:
      "Ohio taxes nonbusiness income on graduated brackets: nothing up to $26,050, then 2.75%, and 3.125% above $100,000 for 2025. Business income is taxed at a flat 3%. The return starts from federal adjusted gross income and takes off an exemption for each person that shrinks as income rises.",
    scope: 'Covers full-year residents filing the Ohio IT 1040. The Schedule of Adjustments, Schedule of Business Income and Schedule of Credits are entered as totals. School district (SD 100) and municipal income taxes are separate returns and are not documented.',
    form: 'oh-form-it1040',
    formSource: 'oh-dot-it-1040-2025',
    forms: [
      {
        title: 'Ohio IT 1040',
        subtitle: 'Individual Income Tax Return',
        rule: 'oh-form-it1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax-free nonbusiness income', keys: ['OhioYear.zero_bracket_ceiling'] },
      { label: 'Exemption per person', keys: ['OhioYear.exemption_low', 'OhioYear.exemption_high'] },
      { label: 'No exemption from', keys: ['OhioYear.exemption_cutoff'], suffix: ' MAGI' },
      { label: 'Business income rate', keys: ['OhioYear.business_rate_bp'] },
    ],
  },
  nc: {
    summary:
      "North Carolina taxes taxable income at a flat rate of 4.25% for 2025. The return begins with federal adjusted gross income, applies North Carolina standard deductions and child deductions, and figures the net tax after withholding and payments.",
    scope: 'Covers full-year residents filing Form D-400. Part-year residents and nonresidents (Schedule NC-400PN) are not yet documented.',
    form: 'nc-form-d400',
    formSource: 'nc-dor-form-d-400-2025',
    forms: [
      {
        title: 'Form D-400',
        subtitle: 'Individual Income Tax Return',
        rule: 'nc-form-d400',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['NorthCarolinaYear.flat_rate_bp'] },
      { label: 'Standard deduction', keys: ['NorthCarolinaYear.standard_deduction_single', 'NorthCarolinaYear.standard_deduction_joint'] },
    ],
  },
  co: {
    summary:
      "Colorado taxes taxable income at a statutory flat rate of 4.40% for 2025. The return starts from federal taxable income, applies Colorado additions and subtractions on Form DR 0104AD, and figures net Colorado income tax liability.",
    scope: 'Covers full-year residents filing Form DR 0104. Part-year residents and nonresidents (Form DR 0104PN) are not yet documented.',
    form: 'co-form-dr0104',
    formSource: 'co-dor-form-dr-0104-2025',
    forms: [
      {
        title: 'Form DR 0104',
        subtitle: 'Individual Income Tax Return',
        rule: 'co-form-dr0104',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['ColoradoYear.flat_rate_bp'] },
      { label: 'Itemized deduction addback threshold', keys: ['ColoradoYear.deduction_addback_agi_threshold'], suffix: ' AGI' },
    ],
  },
  ks: {
    summary:
      "Kansas taxes taxable income at two rates for 2025: 5.2% up to $23,000 ($46,000 married filing jointly) and 5.58% above. The return begins with federal adjusted gross income, applies Schedule S modifications, subtracts the Kansas standard deduction and exemption allowance, and allows a 17% earned income tax credit.",
    scope: 'Covers full-year residents filing Form K-40. Part-year residents and nonresidents (Schedule S Part B) are not yet documented.',
    form: 'ks-form-k40',
    formSource: 'ks-dor-form-k-40-2025',
    forms: [
      {
        title: 'Form K-40',
        subtitle: 'Kansas Individual Income Tax',
        rule: 'ks-form-k40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rates', keys: ['KansasYear.lower_rate_bp', 'KansasYear.upper_rate_bp'] },
      { label: 'Standard deduction', keys: ['KansasYear.standard_deduction_single', 'KansasYear.standard_deduction_joint'] },
      { label: 'Exemption allowance', keys: ['KansasYear.personal_exemption_single', 'KansasYear.personal_exemption_joint'] },
    ],
  },
  az: {
    summary:
      "Arizona taxes taxable income at a flat rate of 2.50% for 2025. The return begins with federal adjusted gross income, applies Arizona additions and subtractions, allows the Arizona standard deduction or itemized deductions, and calculates dependent tax credits.",
    scope: 'Covers full-year residents filing Form 140. Nonresidents (Form 140NR) and part-year residents (Form 140PY) are not yet documented.',
    form: 'az-form-140',
    formSource: 'az-dor-form-140-2025',
    forms: [
      {
        title: 'Form 140',
        subtitle: 'Resident Personal Income Tax Return',
        rule: 'az-form-140',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['ArizonaYear.flat_rate_bp'] },
      { label: 'Standard deduction', keys: ['ArizonaYear.standard_deduction_single', 'ArizonaYear.standard_deduction_joint'] },
      { label: 'Dependent credit (under 17)', keys: ['ArizonaYear.dependent_credit_under_17'] },
    ],
  },
  ga: {
    summary:
      "Georgia taxes taxable income at a flat rate of 5.19% for 2025. The return starts from federal adjusted gross income, subtracts Social Security and the retirement income exclusion on Schedule 1, and takes off the standard or itemized deduction and $4,000 for each dependent.",
    scope: 'Covers full-year residents filing Form 500. Part-year residents and nonresidents (Schedule 3) and the Low Income Credit are not yet documented.',
    form: 'ga-form-500',
    formSource: 'ga-dor-form-500-2025',
    forms: [
      {
        title: 'Form 500',
        subtitle: 'Individual Income Tax Return',
        rule: 'ga-form-500',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['GeorgiaYear.flat_rate_bp'] },
      { label: 'Standard deduction', keys: ['GeorgiaYear.standard_deduction_single', 'GeorgiaYear.standard_deduction_joint'] },
      { label: 'Dependent exemption', keys: ['GeorgiaYear.dependent_exemption'] },
      { label: 'Retirement income exclusion (65 or older)', keys: ['GeorgiaYear.retirement_exclusion_65_and_over'] },
    ],
  },
  id: {
    summary:
      "Idaho taxes taxable income above a zero-rate amount at a single rate of 5.3% for 2025. The return begins with federal adjusted gross income, applies Idaho additions and subtractions, takes the federal standard or itemized deduction (less state income taxes), then the qualified business income and Schedule 1-A deductions, and allows the child tax credit and the refundable food tax credit.",
    scope: 'Covers full-year residents filing Form 40. Part-year residents and nonresidents (Form 43) are not yet documented.',
    form: 'id-form-40',
    formSource: 'id-istc-form-40-2025',
    forms: [
      {
        title: 'Form 40',
        subtitle: 'Individual Income Tax Return',
        rule: 'id-form-40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['IdahoYear.rate_bp'] },
      { label: 'Zero-rate amount', keys: ['IdahoYear.zero_rate_single', 'IdahoYear.zero_rate_joint'] },
      { label: 'Standard deduction', keys: ['IdahoYear.standard_deduction_single', 'IdahoYear.standard_deduction_joint'] },
      { label: 'Child tax credit', keys: ['IdahoYear.child_credit'] },
      { label: 'Food tax credit', keys: ['IdahoYear.food_tax_credit'] },
    ],
  },
  ky: {
    summary:
      "Kentucky taxes taxable income at a flat rate of 4% for 2025. The return begins with federal adjusted gross income, applies Schedule M additions and subtractions (social security is not taxed), allows a $3,270 standard deduction or Kentucky itemized deductions, and reduces the tax by personal tax credits, the family size tax credit for lower incomes, and 20% of the federal child and dependent care credit.",
    scope: 'Covers full-year residents filing Form 740 singly, jointly or as separate returns. A married couple filing separately on one combined return (status 2, Column A), nonresidents and part-year residents (Form 740-NP), and local occupational license taxes are not yet documented.',
    form: 'ky-form-740',
    formSource: 'ky-dor-form-740-2025',
    forms: [
      {
        title: 'Form 740',
        subtitle: 'Individual Income Tax Return, Residents Only',
        rule: 'ky-form-740',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['KentuckyYear.flat_rate_bp'] },
      { label: 'Standard deduction', keys: ['KentuckyYear.standard_deduction'] },
      { label: 'Child and dependent care credit rate', keys: ['KentuckyYear.care_credit_rate_bp'] },
    ],
  },
  la: {
    summary:
      "Louisiana taxes taxable income at a flat rate of 3% from 2025. The return begins with federal adjusted gross income, exempts Social Security and up to $12,000 of retirement income for each filer 65 or older on Schedule E, subtracts the standard deduction and any federal medical deduction beyond the federal standard deduction, and adds a refundable earned income credit of 5% of the federal one.",
    scope: 'Covers full-year residents filing Form IT-540. Nonresidents and part-year residents (Form IT-540B) are not yet documented, nor are the child care and school readiness credits.',
    form: 'la-form-it540',
    formSource: 'la-ldr-form-it-540-2025',
    forms: [
      {
        title: 'Form IT-540',
        subtitle: 'Resident Income Tax Return',
        rule: 'la-form-it540',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['LouisianaYear.flat_rate_bp'] },
      { label: 'Standard deduction', keys: ['LouisianaYear.standard_deduction_single', 'LouisianaYear.standard_deduction_joint'] },
      { label: 'Retirement income exemption (65+)', keys: ['LouisianaYear.retirement_exemption_per_person'] },
    ],
  },
  ms: {
    summary:
      "Mississippi taxes taxable income above $10,000 at 4.4% for 2025. The return rebuilds income from the federal lines, exempts qualified retirement income and social security, and takes off a standard or itemized deduction and fixed exemptions for the filing status, dependents, age and blindness.",
    scope: 'Covers full-year residents filing Form 80-105, prepared as a joint return with everything in column A. Combined returns (each spouse in their own column), non-residents and part-year residents (Form 80-205) are not yet documented.',
    form: 'ms-form-80105',
    formSource: 'ms-dor-form-80-105-2025',
    forms: [
      {
        title: 'Form 80-105',
        subtitle: 'Resident Individual Income Tax Return',
        rule: 'ms-form-80105',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['MississippiYear.rate_bp'] },
      { label: 'Untaxed first', keys: ['MississippiYear.zero_bracket'] },
      { label: 'Standard deduction', keys: ['MississippiYear.standard_deduction_single', 'MississippiYear.standard_deduction_joint'] },
    ],
  },
  ut: {
    summary:
      "Utah taxes income at a flat rate of 4.5% for 2025. The return begins with federal adjusted gross income and has no deduction of its own. Instead the taxpayer tax credit gives back 6% of the federal deduction and the Utah exemption for dependents, less 1.3% of income above a base amount. The child tax credit and earned income credit are figured too.",
    scope: 'Covers full-year residents filing Form TC-40. Nonresidents and part-year residents (Schedule TC-40B), and most TC-40A credits, are not yet documented.',
    form: 'ut-form-tc40',
    formSource: 'ut-usc-form-tc-40-2025',
    forms: [
      {
        title: 'Form TC-40',
        subtitle: 'Individual Income Tax Return',
        rule: 'ut-form-tc40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rate', keys: ['UtahYear.flat_rate_bp'] },
      { label: 'Personal exemption (per dependent)', keys: ['UtahYear.personal_exemption'] },
      { label: 'Credit phase-out base', keys: ['UtahYear.phase_out_base_single', 'UtahYear.phase_out_base_joint'] },
    ],
  },
  ok: {
    summary:
      "Oklahoma taxes taxable income at graduated rates from 0.25% to 4.75% for 2025, through a tax table up to $100,000. The return begins with federal adjusted gross income, applies Oklahoma subtractions, additions and adjustments, allows the Oklahoma standard deduction or itemized deductions and $1,000 for each exemption, and allows child, child care and earned income credits.",
    scope: 'Covers full-year residents filing Form 511 without out-of-state income. Nonresidents and part-year residents (Form 511-NR) are not yet documented.',
    form: 'ok-form-511',
    formSource: 'ok-otc-form-511-2025',
    forms: [
      {
        title: 'Form 511',
        subtitle: 'Resident Income Tax Return',
        rule: 'ok-form-511',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['OklahomaYear.standard_deduction_single', 'OklahomaYear.standard_deduction_joint'] },
      { label: 'Exemption', keys: ['OklahomaYear.exemption_amount'] },
    ],
  },
  ar: {
    summary:
      "Arkansas taxes net taxable income through its Regular Income Tax Table for 2025, with a top rate of 3.9%. The return lists each kind of income as Arkansas counts it, leaving out social security and military pay and excluding the first $6,000 of retirement income, takes the standard deduction, and subtracts personal tax credits of $29 for each filer, dependent and qualifying box.",
    scope: 'Covers full-year residents filing Form AR1000F, column A only, with the standard deduction and the regular tax table. Married filing separately on the same return (status 4), the low income tables, itemized deductions, the capital gains exclusion, part-year residents and nonresidents (Form AR1000NR) are not yet documented.',
    form: 'ar-form-ar1000f',
    formSource: 'ar-dfa-form-ar1000f-2025',
    forms: [
      {
        title: 'Form AR1000F',
        subtitle: 'Individual Income Tax Return, Full Year Resident',
        rule: 'ar-form-ar1000f',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Top rate', keys: ['ArkansasYear.top_rate_bp'] },
      { label: 'Standard deduction', keys: ['ArkansasYear.standard_deduction'] },
      { label: 'Personal tax credit', keys: ['ArkansasYear.personal_credit'] },
    ],
  },
  mo: {
    summary:
      "Missouri taxes taxable income through a chart rising to 4.7% above $9,191 for 2025. The return begins with federal adjusted gross income, deducts part of the federal income tax and the federal standard deduction, and on a combined return figures a separate tax for each spouse.",
    scope: 'Covers full-year residents filing Form MO-1040. Nonresidents and part-year residents (Form MO-NRI), and the Kansas City and St. Louis earnings taxes, are not yet documented.',
    form: 'mo-form-mo1040',
    formSource: 'mo-dor-form-mo-1040-2025',
    forms: [
      {
        title: 'Form MO-1040',
        subtitle: 'Individual Income Tax Return',
        rule: 'mo-form-mo1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Additional exemption, head of household', keys: ['MissouriYear.head_of_household_exemption'] },
      { label: 'Federal tax deduction limit', keys: ['MissouriYear.federal_tax_deduction_limit', 'MissouriYear.federal_tax_deduction_limit_combined'] },
      { label: 'Working Family Tax Credit rate', keys: ['MissouriYear.working_family_credit_rate_bp'] },
    ],
  },
  nd: {
    summary:
      "North Dakota starts from federal taxable income, so the federal standard or itemized deduction carries over. It subtracts 40% of net long-term capital gains and qualified dividends and the taxable part of social security, then taxes the rest at 0%, 1.95% and 2.50% from the tax table or rate schedules. Joint filers with two earners may take a marriage penalty credit.",
    scope: 'Covers full-year residents filing Form ND-1. Nonresidents and part-year residents (Schedule ND-1NR) are not yet documented.',
    form: 'nd-form-nd1',
    formSource: 'nd-tax-form-nd-1-2025',
    forms: [
      {
        title: 'Form ND-1',
        subtitle: 'Individual Income Tax Return',
        rule: 'nd-form-nd1',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax table used below', keys: ['NorthDakotaYear.tax_table_limit'], suffix: ' taxable income' },
      { label: 'Maximum marriage penalty credit', keys: ['NorthDakotaYear.marriage_credit_max'] },
    ],
  },
  ne: {
    summary:
      "Nebraska taxes income at graduated rates from 2.46% to 5.20% for 2025. The return begins with federal adjusted gross income, subtracts the larger of the Nebraska standard deduction or federal itemized deductions less state income taxes, applies Schedule I adjustments, and allows a $171 personal exemption credit for each exemption.",
    scope: 'Covers full-year residents filing Form 1040N. Partial-year residents and nonresidents (Schedule III) are not yet documented.',
    form: 'ne-form-1040n',
    formSource: 'ne-dor-form-1040n-2025',
    forms: [
      {
        title: 'Form 1040N',
        subtitle: 'Individual Income Tax Return',
        rule: 'ne-form-1040n',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['NebraskaYear.standard_deduction_single', 'NebraskaYear.standard_deduction_joint'] },
      { label: 'Personal exemption credit', keys: ['NebraskaYear.personal_exemption_credit'], suffix: ' each' },
      { label: 'Earned income credit', keys: ['NebraskaYear.eitc_rate_bp'], suffix: ' of the federal credit' },
    ],
  },
  va: {
    summary:
      'Virginia taxes taxable income at graduated rates from 2% to 5.75%. The return starts from federal adjusted gross income, subtracts taxable social security and other Schedule ADJ subtractions, and allows a standard or itemized deduction and personal exemptions.',
    scope: 'Covers full-year residents filing Form 760. The age deduction, spouse tax adjustment, Schedule ADJ and credits are entered rather than computed. Part-year residents (Form 760PY) and nonresidents (Form 763) are not yet documented.',
    form: 'va-form-760',
    formSource: 'va-tax-form-760-2025',
    forms: [
      {
        title: 'Form 760',
        subtitle: 'Resident Income Tax Return',
        rule: 'va-form-760',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['VirginiaYear.standard_deduction_single', 'VirginiaYear.standard_deduction_joint'] },
      { label: 'Personal exemption', keys: ['VirginiaYear.exemption_section_a'] },
    ],
  },
  nm: {
    summary:
      "New Mexico taxes taxable income at graduated rates from 1.5% to 5.9% for 2025. The return begins with federal adjusted gross income, takes off the federal standard or itemized deduction, a deduction for certain dependents and the low- and middle-income exemption, and refunds the working families tax credit.",
    scope: 'Covers full-year residents filing Form PIT-1. Part-year residents and nonresidents (Schedule PIT-B), Schedule PIT-ADJ and Schedule PIT-RC are not yet documented.',
    form: 'nm-form-pit1',
    formSource: 'nm-trd-form-pit-1-2025',
    forms: [
      {
        title: 'Form PIT-1',
        subtitle: 'Personal Income Tax Return',
        rule: 'nm-form-pit1',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Deduction for certain dependents', keys: ['NewMexicoYear.certain_dependent_deduction'], suffix: ' each' },
      { label: 'Low- and middle-income exemption', keys: ['NewMexicoYear.lmi_exemption_max'], suffix: ' each' },
      { label: 'Working families tax credit rate', keys: ['NewMexicoYear.wftc_rate_bp'] },
    ],
  },
  sc: {
    summary:
      "South Carolina taxes income subject to tax at 0%, 3% and 6% for 2025. The return begins with federal taxable income, adds and subtracts South Carolina modifications, including a dependent exemption and deductions for retirement income and taxpayers age 65 or older, and takes the tax from the SC1040TT tables or rate schedule.",
    scope: 'Covers full-year residents filing the SC1040. Part-year residents and nonresidents (Schedule NR) are not yet documented.',
    form: 'sc-form-sc1040',
    formSource: 'sc-dor-sc1040-2025',
    forms: [
      {
        title: 'SC1040',
        subtitle: 'Individual Income Tax Return',
        rule: 'sc-form-sc1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Top tax rate', keys: ['SouthCarolinaYear.top_rate_bp'] },
      { label: 'Dependent exemption', keys: ['SouthCarolinaYear.dependent_exemption'], suffix: ' each' },
      { label: 'Age 65 and older deduction', keys: ['SouthCarolinaYear.age_65_deduction'] },
    ],
  },
  dc: {
    summary:
      "The District of Columbia taxes taxable income at graduated rates from 4% to 10.75%. The return begins with federal adjusted gross income, adds and subtracts to reach DC adjusted gross income, takes the District's own standard deduction or itemized deductions without income taxes, and allows 32% of the federal child care credit and 100% of the federal earned income credit.",
    scope: 'Covers full-year residents filing Form D-40. Part-year residents, Schedule H and the Schedule U credits are taken as inputs or not yet documented.',
    form: 'dc-form-d40',
    formSource: 'dc-otr-form-d-40-booklet-2025',
    forms: [
      {
        title: 'Form D-40',
        subtitle: 'Individual Income Tax Return',
        rule: 'dc-form-d40',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['DistrictOfColumbiaYear.standard_deduction_single', 'DistrictOfColumbiaYear.standard_deduction_joint'] },
      { label: 'Child care credit rate', keys: ['DistrictOfColumbiaYear.child_care_credit_rate_bp'] },
      { label: 'Earned income credit rate', keys: ['DistrictOfColumbiaYear.earned_income_credit_rate_bp'] },
    ],
  },
  wv: {
    summary:
      "West Virginia taxes taxable income at graduated rates from 2.22% to 4.82% for 2025. The return begins with federal adjusted gross income, applies Schedule M modifications including the Social Security and senior citizen subtractions, takes the low-income earned income exclusion and $2,000 for each exemption, and finds the tax in the Tax Table or a Rate Schedule.",
    scope: 'Covers full-year residents filing Form IT-140. Nonresidents and part-year residents (Schedule A) are not yet documented.',
    form: 'wv-form-it140',
    formSource: 'wv-tax-it-140-booklet-2025',
    forms: [
      {
        title: 'Form IT-140',
        subtitle: 'Personal Income Tax Return',
        rule: 'wv-form-it140',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Exemption', keys: ['WestVirginiaYear.exemption'] },
      { label: 'Low-income exclusion limit', keys: ['WestVirginiaYear.low_income_limit'], suffix: ' AGI' },
    ],
  },
  me: {
    summary:
      "Maine taxes taxable income at graduated rates of 5.8%, 6.75% and 7.15%. The return begins with federal adjusted gross income, applies Maine's addition and subtraction modifications, and takes a standard or itemized deduction and a personal exemption, both of which phase out at higher incomes.",
    scope: 'Covers full-year residents filing Form 1040ME. Schedule A credits, the property and sales tax fairness credits and the modifications are inputs. Nonresidents and part-year residents (Schedule NR) are not yet documented.',
    form: 'me-form-1040me',
    formSource: 'me-mrs-form-1040me-2025',
    forms: [
      {
        title: 'Form 1040ME',
        subtitle: 'Maine Individual Income Tax Return',
        rule: 'me-form-1040me',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['MaineYear.standard_deduction_single', 'MaineYear.standard_deduction_joint'] },
      { label: 'Personal exemption', keys: ['MaineYear.personal_exemption'] },
    ],
  },
  hi: {
    summary:
      "Hawaii taxes taxable income at graduated rates from 1.4% to 11% for 2025. The return begins with federal adjusted gross income, applies Hawaii additions and subtractions, takes the Hawaii standard or itemized deduction and $1,144 for each exemption, and subtracts refundable credits before nonrefundable ones.",
    scope: 'Covers full-year residents filing Form N-11. Part-year residents and nonresidents (Form N-15) are not yet documented. Credits are entered as their own forms figure them.',
    form: 'hi-form-n11',
    formSource: 'hi-dotax-form-n-11-2025',
    forms: [
      {
        title: 'Form N-11',
        subtitle: 'Individual Income Tax Return (Resident)',
        rule: 'hi-form-n11',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['HawaiiYear.standard_deduction_single', 'HawaiiYear.standard_deduction_joint'] },
      { label: 'Exemption', keys: ['HawaiiYear.exemption_amount'] },
    ],
  },
  vt: {
    summary:
      "Vermont taxes taxable income at graduated rates from 3.35% to 8.75%. The return begins with federal adjusted gross income, applies Vermont's net modifications, subtracts a standard deduction and personal exemptions, and above $150,000 of federal AGI charges at least 3% of it. Every filer gets a credit of 5% of charitable contributions, up to $1,000.",
    scope: 'Covers full-year residents filing Form IN-111. Modifications, credits from Schedules IN-117 and IN-119, refundable credits, the property tax credit and the renter credit are taken as inputs. Part-year residents and nonresidents (Schedule IN-113) are not yet documented.',
    form: 'vt-form-in111',
    formSource: 'vt-tax-form-in-111-2025',
    forms: [
      {
        title: 'Form IN-111',
        subtitle: 'Vermont Income Tax Return',
        rule: 'vt-form-in111',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['VermontYear.standard_deduction_single', 'VermontYear.standard_deduction_joint'] },
      { label: 'Personal exemption', keys: ['VermontYear.personal_exemption'] },
      { label: 'Minimum tax applies above', keys: ['VermontYear.minimum_tax_agi_threshold'], suffix: ' AGI' },
    ],
  },
  de: {
    summary:
      "Delaware taxes taxable income through a graduated rate schedule, from 2.2% above $2,000 to 6.6% above $60,000. The return begins with federal adjusted gross income, applies Delaware additions and subtractions, allows the Delaware standard deduction or itemized deductions, and takes personal, child care and earned income credits off the tax.",
    scope: 'Covers full-year residents filing Form PIT-RES, Column B. Filing status 4 (combined separate), the tax table below $60,000 of taxable income, nonresidents (Form PIT-NON) and the Wilmington city wage tax are not yet documented.',
    form: 'de-form-pit-res',
    formSource: 'de-dor-form-pit-res-2025',
    forms: [
      {
        title: 'Form PIT-RES',
        subtitle: 'Individual Resident Income Tax Return',
        rule: 'de-form-pit-res',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Standard deduction', keys: ['DelawareYear.standard_deduction_single', 'DelawareYear.standard_deduction_joint'] },
      { label: 'Personal credit', keys: ['DelawareYear.personal_credit'] },
      { label: 'Refundable EITC rate', keys: ['DelawareYear.refundable_eitc_rate_bp'] },
    ],
  },
  ct: {
    summary:
      "Connecticut taxes income through seven graduated rates, from 2% to 6.99% for 2025. The return starts from federal adjusted gross income, adjusts it on Schedule 1 to Connecticut adjusted gross income, and reads the tax from the Tax Calculation Schedule, which folds in the personal exemption, the 3% phase-out, the benefit recapture and the personal tax credit.",
    scope: "Covers full-year residents filing Form CT-1040. The personal exemption, phase-out, recapture and personal tax credit are read from the instructions' tables and taken as inputs; nonresidents and part-year residents (Form CT-1040NR/PY) are not yet documented.",
    form: 'ct-form-ct1040',
    formSource: 'ct-drs-form-ct-1040-2025',
    forms: [
      {
        title: 'Form CT-1040',
        subtitle: 'Connecticut Resident Income Tax Return',
        rule: 'ct-form-ct1040',
        walkthrough: '',
      },
    ],
    glance: [],
  },
  ri: {
    summary:
      'Rhode Island taxes taxable income at 3.75%, 4.75% and 5.99% for 2025. The return begins with federal adjusted gross income, applies the Schedule M modifications, and takes off a Rhode Island standard deduction and $5,100 for each exemption. Federal itemized deductions are not allowed.',
    scope:
      'Covers full-year residents filing Form RI-1040 with modified federal AGI up to $254,250, using the Tax Computation Worksheet. Nonresidents (Form RI-1040NR), the exemption phase-out worksheet and most credits are not yet documented.',
    form: 'ri-form-ri1040',
    formSource: 'ri-dor-form-ri-1040-instructions-2025',
    forms: [
      {
        title: 'Form RI-1040',
        subtitle: 'Resident Individual Income Tax Return',
        rule: 'ri-form-ri1040',
        walkthrough: '',
      },
    ],
    glance: [
      { label: 'Tax rates', keys: ['RhodeIslandYear.rate_1_bp', 'RhodeIslandYear.rate_2_bp', 'RhodeIslandYear.rate_3_bp'] },
      { label: 'Standard deduction', keys: ['RhodeIslandYear.standard_deduction_single', 'RhodeIslandYear.standard_deduction_joint'] },
      { label: 'Exemption', keys: ['RhodeIslandYear.exemption_amount'] },
    ],
  },
};

/** Where a jurisdiction's page is. */
export function jurisdictionPath(code: string, year: number): string[] {
  return ['/', String(year), code.toLowerCase()];
}
