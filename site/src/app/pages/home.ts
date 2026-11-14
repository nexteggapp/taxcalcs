import { ChangeDetectionStrategy, Component, computed, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { model } from '../model';
import { Jurisdiction, documented, kindLabels, states } from '../jurisdictions';
import { formCoverage, sourcesFor, totals } from '../tree';
import { How } from '../how';

@Component({
  selector: 'app-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, How],
  template: `
    <header class="hero">
      <p class="eyebrow">taxcalcs</p>
      <h1 class="headline">An open-source tax calculation library</h1>
      <p class="lede">
        taxcalcs prepares federal and state income tax returns and estimates quarterly tax for all 50 states and DC.
      </p>
      <p class="lede">
        These pages document how it works inside, so you can audit its calculations and see how each jurisdiction
        taxes income. Every line is shown the way the form computes it, with the worksheets and tests behind it, the
        law it rests on, and the page of the publication that prints each figure.
      </p>
      <p class="stats">
        <span><b>{{ docCount }}</b> {{ docCount === 1 ? 'jurisdiction' : 'jurisdictions' }} documented</span>
        <span><b>{{ t.linesComputed }}</b> form lines computed</span>
        <span><b>{{ t.figures }}</b> figures, each linked to its source</span>
        <span><b>{{ t.leftOut }}</b> parts left out, each with a reason</span>
      </p>
    </header>

    <section class="fed">
      <span class="fed-icon" aria-hidden="true">US</span>
      <div class="fed-text">
        <strong>Federal</strong>
        <span class="muted small">Form 1040 and its schedules, the return every state return starts from</span>
      </div>
      <a class="button" routerLink="/2025/us">Open federal 2025 →</a>
    </section>

    <section>
      <h2>States</h2>
      <div class="map-wrap">
        <div class="map-col">
        <div class="map" role="group" aria-label="States">
          @for (cell of cells; track $index) {
            @if (cell; as j) {
              <button
                type="button"
                class="tile"
                [class]="'tile tile-' + j.kind"
                [class.sel]="j.code === selected().code"
                [class.documented]="isDocumented(j)"
                [attr.aria-label]="j.name"
                [attr.aria-pressed]="j.code === selected().code"
                [title]="j.name"
                (click)="pick(j)"
              >
                {{ j.code }}
              </button>
            } @else {
              <span></span>
            }
          }
        </div>
        <div class="legend">
          <span><i class="tile-graduated"></i>Graduated rates</span>
          <span><i class="tile-flat"></i>Flat rate</span>
          <span><i class="tile-none"></i>No tax on wages</span>
          <span><i class="legend-doc"></i>Documented</span>
        </div>
        </div>
        <div class="panel">
          @let j = selected();
          <strong class="panel-name">{{ j.name }}</strong>
          <span class="muted small">{{ kindLabels[j.kind] }}</span>
          @if (isDocumented(j)) {
            @let c = coverageOf(j);
            <ul class="panel-facts">
              <li>{{ j.form }} and {{ formCount(j) - 1 }} more forms</li>
              <li>Computes {{ c.computed }} of {{ c.total }} lines · {{ c.leftOut }} left out</li>
              <li>{{ sourceCount(j) }} sources, every figure linked</li>
            </ul>
            <a class="cta block" [routerLink]="['/2025', j.code.toLowerCase()]">Open {{ j.name }} 2025 →</a>
          } @else if (j.kind === 'none') {
            <p class="small">{{ j.name }} has no income tax on wages, so taxcalcs prepares no return for it.</p>
            <a class="button block" [routerLink]="['/2025', j.code.toLowerCase()]">Open {{ j.name }} 2025 →</a>
          } @else {
            <p class="small">
              taxcalcs estimates {{ j.name }}'s quarterly tax. Its {{ j.form }} is not yet prepared from documented
              rules.
            </p>
            <a class="button block" [routerLink]="['/2025', j.code.toLowerCase()]">Open {{ j.name }} 2025 →</a>
          }
        </div>
      </div>
    </section>

    <app-how />
  `,
})
export class Home {
  protected readonly kindLabels = kindLabels;
  protected readonly t = totals();
  protected readonly docCount = Object.keys(documented).length;
  protected readonly cells: (Jurisdiction | undefined)[] = Array.from({ length: 88 }, (_, i) =>
    states.find((s) => s.at[0] === i % 11 && s.at[1] === Math.floor(i / 11)),
  );
  private readonly code = signal('CA');
  protected readonly selected = computed(() => states.find((s) => s.code === this.code())!);

  protected pick(j: Jurisdiction): void {
    this.code.set(j.code);
  }
  protected isDocumented(j: Jurisdiction): boolean {
    return j.code.toLowerCase() in documented;
  }
  protected coverageOf(j: Jurisdiction) {
    return formCoverage(documented[j.code.toLowerCase()].formSource);
  }
  protected formCount(j: Jurisdiction): number {
    return documented[j.code.toLowerCase()].forms.length;
  }
  protected sourceCount(j: Jurisdiction): number {
    return sourcesFor(j.code.toLowerCase(), model.year).length;
  }
}
