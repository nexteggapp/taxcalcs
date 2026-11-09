import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { pageTitle } from '../page-title';

@Component({
  selector: 'app-not-found',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <h1>There is no page here</h1>
    <p class="doc">
      The link may be to a rule that has been renamed, or to a jurisdiction or year these pages do not cover yet.
    </p>
    <p><a class="cta" routerLink="/">Choose a jurisdiction →</a> <a class="button" routerLink="/rules">Search the rules</a></p>
  `,
})
export class NotFound {
  constructor() {
    pageTitle(() => 'Not found');
  }
}
