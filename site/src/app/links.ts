import { Directive, inject } from '@angular/core';
import { Router } from '@angular/router';

/**
 * Sends clicks on links inside generated HTML (formulas, derivations)
 * through the router, so the page does not reload. Those links are written
 * relative to the base, "rules/ca-form-540", so they also work without
 * scripts wherever the site is served.
 */
@Directive({
  selector: '[appLinks]',
  host: { '(click)': 'onClick($event)' },
})
export class Links {
  private readonly router = inject(Router);

  protected onClick(e: MouseEvent): void {
    const a = (e.target as HTMLElement).closest('a');
    const href = a?.getAttribute('href');
    if (!a || !href || /^([a-z]+:|#|\/\/)/i.test(href) || e.metaKey || e.ctrlKey || e.shiftKey || a.hasAttribute('download')) {
      return;
    }
    if (href.startsWith('pdfs/')) return;
    e.preventDefault();
    this.router.navigateByUrl(href.startsWith('/') ? href : '/' + href);
  }
}
