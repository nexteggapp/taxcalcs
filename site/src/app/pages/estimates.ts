import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Crumbs } from '../crumbs';
import { pageTitle } from '../page-title';
import { Prose } from '../prose';

const repo = 'https://github.com/nexteggapp/taxcalcs/blob/main';

/** The tax-year estimator, taxyear: a layer built on the library. */
@Component({
  selector: 'app-estimates',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs, Prose],
  template: `
    <app-crumbs [items]="[{ label: 'Home', link: ['/'] }, { label: 'Tax-year estimates' }]" />
    <p class="eyebrow">A layer built on the library</p>
    <h1>Estimating a tax year in progress</h1>
    <p class="lede">
      The library prepares returns from a year’s facts. Most of the year, those facts are not known yet: there are
      pay stubs so far, and plans for the rest. The estimator, the taxyear package, fills that gap. It projects the
      whole year from what has happened and what the household expects, and hands each projection to the library to
      prepare.
    </p>
    <p class="doc">
      <app-prose
        text="It is documented apart from the library because it answers a different question. The library answers “what does the law say about these [facts]?” and is exact. The estimator answers “what will this year probably look like?”, and is only as good as its assumptions, so it keeps every assumption visible."
      />
    </p>

    <h2>Principles</h2>
    <dl class="principles">
      <dt>Evidence first</dt>
      <dd>
        The file’s as_of date divides the year. An amount read from evidence, such as a pay stub or a bank deposit, is
        made. Anything else is expected, and says which assumption it came from. Evidence wins wherever it exists.
      </dd>
      <dt>Models, not totals</dt>
      <dd>
        Each source of income, deduction or payment is a model: a job, a bank account, a mortgage, tax paid to a
        jurisdiction. A model is made of parts, such as “from these pay stubs”, “a raise from October” or “the same as
        the latest paycheck”, and expands into dated items: one paycheck, one month’s interest.
      </dd>
      <dt>Scenarios are diffs</dt>
      <dd>
        Your plan comes first. Every other scenario, such as “raise in October”, is stored as a change to its parent,
        so the evidence is entered once and shared by every scenario, and two scenarios’ items compare one to one.
      </dd>
      <dt>The library does the tax</dt>
      <dd>
        <app-prose
          text="The estimator works out no tax itself. It turns each scenario’s projection into \`ReturnInputs\`, one document per model with its full-year figures, and the library prepares the [return]. Estimated payments and the underpayment penalty come from each jurisdiction’s estimated-tax rules."
        />
      </dd>
      <dt>Every figure explains itself, and nothing is guessed</dt>
      <dd>
        Each figure has its working: the figures or items it adds up, the parts and evidence it rests on, and the
        rules applied. When something is missing, such as the filing status, the result says what it assumed and asks.
      </dd>
    </dl>

    <h2>Typical use</h2>
    <ol class="usage">
      <li>
        Write the year as a TaxYearScenarios file: the household’s facts, a model of each source of income and
        payment with its evidence, and the scenarios. The file holds inputs only; no figure in it is worked out.
      </li>
      <li>Compute it, from Go or from the command line:</li>
    </ol>
    <figure class="example">
      <pre class="code"><code>file, err := taxyear.Parse(jsonc) // JSON with comments
result, err := taxyear.Compute(file)

plan, _ := result.Scenario("plan")
path, _ := taxyear.ParsePath("federal.form_1040.total_tax")
f, _ := plan.Figure(path)
amount, _ := f.Amount()
working := f.Working()</code></pre>
      <figcaption class="muted small">From docs/results.md</figcaption>
    </figure>
    <figure class="example">
      <pre class="code"><code>bazel run //cmd/taxyear -- --summary "$PWD/taxyear/testdata/example.jsonc"</code></pre>
      <figcaption class="muted small">The taxyear command, on the example file in the repository</figcaption>
    </figure>
    <ol class="usage" start="3">
      <li>
        Read each scenario’s result: the projection of every model into dated items, the returns, an estimated-payment
        answer for each jurisdiction, the questions left open, and the working behind each figure.
      </li>
      <li>
        When new evidence arrives, add it to the file and compute again. A taxyear.Cache reuses the scenarios that did
        not change.
      </li>
    </ol>

    <h2>Read more</h2>
    <ul>
      <li><a [href]="repo + '/docs/models.md'" rel="noopener">Models</a>: each kind of model, its parts, and how a model projects the year</li>
      <li><a [href]="repo + '/docs/scenarios.md'" rel="noopener">Scenarios</a>: the TaxYearScenarios file, and scenarios as diffs</li>
      <li><a [href]="repo + '/docs/results.md'" rel="noopener">Results</a>: the projection, the returns, estimated payments and working</li>
      <li><a [href]="repo + '/docs/estimated-tax.md'" rel="noopener">Estimated tax</a>: estimated payments and the underpayment penalty, state by state</li>
      <li><a [href]="repo + '/docs/taxyear-cli.md'" rel="noopener">The taxyear command</a></li>
    </ul>
    <p><a routerLink="/">← How the library works</a></p>
  `,
})
export class EstimatesPage {
  protected readonly repo = repo;

  constructor() {
    pageTitle(() => 'Tax-year estimates');
  }
}
