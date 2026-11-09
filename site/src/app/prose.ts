import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { prose } from './glossary';
import { Links } from './links';

/**
 * A paragraph of the site's own prose, with its [terms] linked to the
 * glossary and its `Types` to their pages.
 */
@Component({
  selector: 'app-prose',
  changeDetection: ChangeDetectionStrategy.OnPush,
  hostDirectives: [Links],
  template: `<span [innerHTML]="html()"></span>`,
})
export class Prose {
  readonly text = input.required<string>();
  private readonly sanitizer = inject(DomSanitizer);
  // The text is this site's own, and its links carry definitions in data
  // attributes, which the sanitizer would drop.
  protected readonly html = computed(() => this.sanitizer.bypassSecurityTrustHtml(prose(this.text())));
}
