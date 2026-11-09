import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { Crumbs } from '../crumbs';
import spec from '../../model/taxlisp.md';
import { model } from '../model';
import { renderMarkdown } from '../markdown';
import { pageTitle } from '../page-title';

const doc = renderMarkdown(
  // The page has its own title and introduction.
  spec.replace(/^# .*\n/, ''),
);

/** The taxlisp specification, from docs/taxlisp.md. */
@Component({
  selector: 'app-taxlisp',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="crumbs" />
    <div class="spec">
      <aside class="spec-toc" aria-label="Contents">
        <strong class="small">Contents</strong>
        <ul>
          @for (h of headings; track h.id) {
            <li [class.sub]="h.level === 3">
              <a routerLink="/taxlisp" [fragment]="h.id">{{ h.text }}</a>
            </li>
          }
        </ul>
      </aside>
      <article class="spec-body">
        <p class="eyebrow">Language specification</p>
        <h1>The taxlisp language</h1>
        <p class="lede">
          taxlisp is the language taxcalcs’s rules are written in: statically typed, purely functional, with exact
          money, and with every program also a protobuf syntax tree. This is its full specification.
        </p>
        <p class="note small">
          Every rule on this site is taxlisp: <a routerLink="/rules">the {{ ruleCount }} rules</a> each show their
          source, and the walkthroughs show the trace of a run. The specification is kept in the repository as
          <a href="https://github.com/nexteggapp/taxcalcs/blob/main/docs/taxlisp.md" rel="noopener">docs/taxlisp.md</a>.
        </p>
        <div class="markdown" [innerHTML]="html"></div>
      </article>
    </div>
  `,
})
export class TaxlispPage {
  // The specification is the repository's own document, rendered at build
  // time; trusting it keeps the headings' ids, which the contents link to.
  protected readonly html = inject(DomSanitizer).bypassSecurityTrustHtml(doc.html);
  protected readonly headings = doc.headings.filter((h) => h.level <= 3);
  protected readonly ruleCount = model.rules.length;
  protected readonly crumbs = [{ label: 'Home', link: ['/'] }, { label: 'taxlisp' }];

  constructor() {
    pageTitle(() => 'The taxlisp language');
  }
}
