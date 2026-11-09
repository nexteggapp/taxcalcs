import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { Crumbs } from '../crumbs';
import { glossaryIntro, prose, renderTerm, terms } from '../glossary';
import { Links } from '../links';
import { pageTitle } from '../page-title';

/** Every term these pages use, A to Z. */
@Component({
  selector: 'app-glossary',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs, Links],
  template: `
    <app-crumbs [items]="crumbs" />
    <h1>Glossary</h1>
    <p class="doc" appLinks [innerHTML]="intro"></p>
    <nav class="glossary-az" aria-label="Terms">
      @for (t of entries; track t.id) {
        <a routerLink="/glossary" [fragment]="t.id">{{ t.term }}</a>
      }
    </nav>
    <dl class="glossary" appLinks>
      @for (t of entries; track t.id) {
        <dt [id]="t.id">{{ t.term }}</dt>
        <dd [innerHTML]="t.html"></dd>
      }
    </dl>
  `,
})
export class GlossaryPage {
  private readonly sanitizer = inject(DomSanitizer);
  protected readonly crumbs = [{ label: 'Home', link: ['/'] }, { label: 'Glossary' }];
  protected readonly intro = this.sanitizer.bypassSecurityTrustHtml(prose(glossaryIntro.replace(/\s+/g, ' ')));
  protected readonly entries = [...terms]
    .sort((a, b) => a.term.localeCompare(b.term))
    .map((t) => ({ ...t, html: this.sanitizer.bypassSecurityTrustHtml(renderTerm(t)) }));

  constructor() {
    pageTitle(() => 'Glossary');
  }
}
