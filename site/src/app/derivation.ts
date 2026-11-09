import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { DerivationNode, rules, slug } from './model';
import { contextFor, humanize, render, summary, title, value } from './format';
import { Links } from './links';

/**
 * The steps that produced a value: the calculations called, a worksheet's
 * lines, and the conditions of a test, each with its result.
 */
@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-derivation',
  imports: [RouterLink, Derivation, Links],
  host: { class: 'deriv-host' },
  template: `
    <ul class="deriv" appLinks>
      @for (n of nodes(); track $index) {
        <li [class]="'step step-' + n.kind">
          @switch (n.kind) {
            @case ('call') {
              <details [open]="open()">
                <summary>
                  <a [routerLink]="['/rules', slug(n.name!)]" class="rule">{{ title(n.name!) }}</a>
                  <span class="deriv-val">{{ value(n.value) }}</span>
                </summary>
                @if (doc(n.name!); as d) {
                  <p class="deriv-doc">{{ d }}</p>
                }
                @if (shownArgs(n).length) {
                  <p class="deriv-args">
                    @for (a of shownArgs(n); track a.name) {
                      <span><var>{{ humanize(a.name) }}</var> {{ value(a.value) }}</span>
                    }
                  </p>
                }
                @if (n.children?.length) {
                  <app-derivation [nodes]="n.children!" [open]="open()" [rule]="n.name" />
                }
              </details>
            }
            @case ('line') {
              <div class="deriv-line">
                <span class="deriv-num">{{ n.line }}</span>
                <span class="deriv-cap">{{ n.caption }}</span>
                <span class="deriv-val">{{ value(n.value) }}</span>
              </div>
              @if (n.children?.length) {
                <app-derivation [nodes]="n.children!" [open]="open()" [rule]="rule()" />
              }
            }
            @case ('check') {
              <div class="deriv-check" [class.yes]="n.value === 'true'" [class.no]="n.value === 'false'">
                <span class="mark" [attr.aria-label]="n.value === 'true' ? 'met' : 'not met'">{{ n.value === 'true' ? '✓' : '✗' }}</span>
                @if (n.expr) {
                  <span class="formula" [innerHTML]="condition(n)"></span>
                } @else {
                  <code>{{ n.caption }}</code>
                }
              </div>
              @if (n.children?.length) {
                <app-derivation [nodes]="n.children!" [open]="open()" [rule]="rule()" />
              }
            }
          }
        </li>
      }
    </ul>
  `,
})
export class Derivation {
  readonly nodes = input.required<DerivationNode[]>();
  readonly open = input(false);
  /** The rule these steps are part of, which says what a check's names mean. */
  readonly rule = input<string>();

  protected readonly slug = slug;
  protected readonly title = title;
  protected readonly value = value;
  protected readonly humanize = humanize;
  private readonly ctx = computed(() => {
    const r = rules.get(this.rule() ?? '');
    return r ? contextFor(r) : { lines: new Map<string, string>(), figureParams: new Map<string, string>() };
  });

  protected doc(name: string): string {
    const r = rules.get(name);
    return r ? summary(r.doc) : '';
  }
  protected shownArgs(n: DerivationNode) {
    return (n.args ?? []).filter((a) => value(a.value) !== '');
  }
  protected condition(n: DerivationNode): string {
    return render(n.expr!, this.ctx());
  }
}
