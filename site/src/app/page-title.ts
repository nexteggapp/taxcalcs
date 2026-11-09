import { effect, inject } from '@angular/core';
import { Title } from '@angular/platform-browser';

/**
 * Keeps the document's title on what the page shows: "AGI Limitation
 * Worksheet · taxcalcs". Call it from a component's field initializer. It
 * also runs when the page is prerendered, so the static HTML has the title.
 */
export function pageTitle(name: () => string | undefined): void {
  const title = inject(Title);
  effect(() => {
    const n = name();
    title.setTitle(n ? `${n} · taxcalcs` : 'taxcalcs');
  });
}
