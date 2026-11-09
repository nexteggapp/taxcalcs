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
      <a routerLink="/" class="brand"><img src="icon.svg" alt="" width="24" height="24" />taxcalcs <span>docs</span></a>
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
