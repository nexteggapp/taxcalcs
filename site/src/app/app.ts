import { ViewportScroller } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { model } from './model';

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  template: `
    <header class="top">
      <a routerLink="/" class="brand">
        <!-- The mark without its tile, in the page's own colours. -->
        <svg class="mark" viewBox="0 0 64 64" width="26" height="26" aria-hidden="true">
          <path d="M8 48L24 30l10 8 22-24" />
          <circle cx="56" cy="14" r="6" />
          <path class="base" d="M8 58h48" />
        </svg>
        taxcalcs <span>docs</span>
      </a>
      <nav>
        <a routerLink="/" routerLinkActive="on" [routerLinkActiveOptions]="{ exact: true }">Jurisdictions</a>
        <a routerLink="/sources" routerLinkActive="on">Sources</a>
        <a routerLink="/figures/ca-year-2025" routerLinkActive="on">Figures</a>
        <a routerLink="/rules" routerLinkActive="on">Rules A–Z</a>
        <a routerLink="/types" routerLinkActive="on">Types</a>
        <a routerLink="/glossary" routerLinkActive="on">Glossary</a>
      </nav>
      <span class="year">Tax year {{ model.year }}</span>
    </header>
    <main>
      <router-outlet />
    </main>
    <footer>
      Generated from the taxlisp rules and the source catalog of
      <a href="https://github.com/nexteggapp/taxcalcs">taxcalcs</a>. This is software documentation, not tax
      advice.
    </footer>
  `,
})
export class App {
  protected readonly model = model;

  constructor() {
    // Links to a heading or a line land below the sticky header.
    inject(ViewportScroller).setOffset([0, 72]);
  }
}
