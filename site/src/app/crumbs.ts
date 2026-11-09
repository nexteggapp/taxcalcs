import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Rule, Source } from './model';
import { byCode } from './jurisdictions';

export interface Crumb {
  label: string;
  link?: string[];
}

/** Where a page sits: Jurisdictions › 2025 › California › … */
@Component({
  selector: 'app-crumbs',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <nav class="crumbs" aria-label="Breadcrumb">
      @for (c of items(); track $index) {
        @if (!$first) {
          <span class="sep" aria-hidden="true">›</span>
        }
        @if (c.link) {
          <a [routerLink]="c.link">{{ c.label }}</a>
        } @else {
          <span aria-current="page">{{ c.label }}</span>
        }
      }
    </nav>
  `,
})
export class Crumbs {
  readonly items = input.required<Crumb[]>();
}

const year = 2025;

/** The crumbs down to a jurisdiction's page. */
export function jurisdictionCrumbs(code: string): Crumb[] {
  const j = byCode.get(code.toLowerCase());
  const name = code.toLowerCase() === 'us' ? 'Federal' : (j?.name ?? code.toUpperCase());
  return [
    { label: 'Jurisdictions', link: ['/'] },
    { label: String(year) },
    { label: name, link: ['/', String(year), code.toLowerCase()] },
  ];
}

/** The jurisdiction a rule belongs to, from its file. */
export function ruleCrumbs(r: Rule): Crumb[] {
  const m = r.file.match(/\/states\/([a-z]{2})\//);
  if (m) return jurisdictionCrumbs(m[1]);
  return [{ label: 'Jurisdictions', link: ['/'] }, { label: 'Shared state rules', link: ['/rules'] }];
}

export function sourceCrumbs(s: Source): Crumb[] {
  return [...jurisdictionCrumbs(s.publisher.jurisdiction), { label: 'Sources', link: ['/sources'] }];
}
