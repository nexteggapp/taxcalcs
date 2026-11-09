import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  afterRenderEffect,
  computed,
  input,
  signal,
  viewChild,
} from '@angular/core';
import { Anchor, Region, Source, pdfPath } from './model';

type PdfDoc = { getPage(n: number): Promise<PdfPage>; numPages: number };
type PdfPage = {
  getViewport(o: { scale: number }): { width: number; height: number };
  render(o: { canvas: HTMLCanvasElement; canvasContext: CanvasRenderingContext2D; viewport: unknown }): {
    promise: Promise<void>;
  };
};

const docs = new Map<string, Promise<PdfDoc>>();

async function load(url: string): Promise<PdfDoc> {
  let doc = docs.get(url);
  if (!doc) {
    doc = import('pdfjs-dist').then((pdfjs) => {
      // pdf.js resolves against the page's address, not the base.
      pdfjs.GlobalWorkerOptions.workerSrc = new URL('pdfjs/pdf.worker.min.mjs', document.baseURI).href;
      return pdfjs.getDocument({ url: new URL(url, document.baseURI).href }).promise as unknown as Promise<PdfDoc>;
    });
    docs.set(url, doc);
    // A failed load is not kept, so a retry starts afresh.
    doc.catch(() => docs.delete(url));
  }
  return doc;
}

/**
 * The part of a source's page an anchor occupies, cut out of the vendored
 * PDF, with the whole page a click away.
 */
@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-excerpt',
  template: `
    @if (url(); as u) {
      <figure class="excerpt">
        <div class="excerpt-frame" [class.whole]="whole()">
          <canvas #canvas></canvas>
          @switch (state()) {
            @case ('loading') {
              <div class="excerpt-wait">Loading page {{ page() }}…</div>
            }
            @case ('error') {
              <div class="excerpt-wait">
                @if (stale()) {
                  <span>The site has been updated since this page loaded.</span>
                  <button type="button" (click)="reload()">Reload the page</button>
                } @else {
                  <span>Page {{ page() }} could not be shown.</span>
                  <button type="button" (click)="retry()">Try again</button>
                }
              </div>
            }
          }
        </div>
        <figcaption>
          <span>{{ source().designation }}, page {{ page() }}</span>
          <span class="excerpt-actions">
            @if (region()) {
              <button type="button" (click)="whole.set(!whole())">{{ whole() ? 'Show the excerpt' : 'Show the whole page' }}</button>
            }
            <a [href]="u + '#page=' + page()" target="_blank" rel="noopener">Open the PDF at this page</a>
            <a [href]="u" [attr.download]="fileName()">Download the PDF</a>
          </span>
        </figcaption>
      </figure>
    } @else {
      <p class="muted">No copy of this source is kept, so there is no page to show.</p>
    }
  `,
})
export class Excerpt {
  readonly source = input.required<Source>();
  readonly anchor = input.required<Anchor>();

  protected readonly whole = signal(false);
  protected readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  protected readonly stale = signal(false);
  private readonly attempt = signal(0);
  protected readonly url = computed(() => pdfPath(this.source()));
  protected readonly page = computed(() => this.anchor().at?.page ?? 1);
  protected readonly region = computed(() => this.anchor().at?.region);
  protected readonly fileName = computed(() => this.url()?.split('/').pop() ?? 'source.pdf');

  private readonly canvas = viewChild<ElementRef<HTMLCanvasElement>>('canvas');
  private run = 0;

  constructor() {
    // Drawing on the canvas is DOM work, which belongs after rendering, and
    // only in the browser: prerendering never runs it.
    afterRenderEffect(() => {
      const url = this.url();
      const page = this.page();
      const region = this.region();
      const whole = this.whole();
      this.attempt();
      const canvas = this.canvas()?.nativeElement;
      if (!url || !canvas) return;
      this.draw(++this.run, canvas, url, page, region, whole);
    });
  }

  private async draw(
    run: number,
    out: HTMLCanvasElement,
    url: string,
    n: number,
    region: Region | undefined,
    whole: boolean,
  ): Promise<void> {
    this.state.set('loading');
    try {
      const doc = await load(url);
      const page = await doc.getPage(n);
      const width = Math.min(out.parentElement?.clientWidth || 720, 900);
      const dpr = window.devicePixelRatio || 1;
      const base = page.getViewport({ scale: 1 });
      const r = region && !whole ? region : undefined;
      const fx = r?.x ?? 0;
      const fy = r?.y ?? 0;
      const fw = r?.width ?? 1;
      const fh = r?.height ?? 1;
      const scale = (width / (base.width * fw)) * dpr;
      const viewport = page.getViewport({ scale });
      const full = document.createElement('canvas');
      full.width = Math.ceil(viewport.width);
      full.height = Math.ceil(viewport.height);
      const ctx = full.getContext('2d')!;
      await page.render({ canvas: full, canvasContext: ctx, viewport }).promise;
      if (run !== this.run) return;
      const sx = fx * full.width;
      const sy = fy * full.height;
      const sw = fw * full.width;
      const sh = fh * full.height;
      out.width = Math.ceil(sw);
      out.height = Math.ceil(sh);
      out.style.width = `${Math.round(sw / dpr)}px`;
      const o = out.getContext('2d')!;
      o.drawImage(full, sx, sy, sw, sh, 0, 0, sw, sh);
      if (whole && region) {
        o.fillStyle = 'rgba(255, 196, 0, 0.18)';
        o.strokeStyle = 'rgba(214, 120, 0, 0.9)';
        o.lineWidth = 2 * dpr;
        const rx = (region.x ?? 0) * full.width;
        const ry = (region.y ?? 0) * full.height;
        const rw = (region.width ?? 1) * full.width;
        const rh = (region.height ?? 1) * full.height;
        o.fillRect(rx, ry, rw, rh);
        o.strokeRect(rx, ry, rw, rh);
      }
      this.state.set('ready');
    } catch (e) {
      console.error(`Could not show page ${n} of ${url}`, e);
      if (run !== this.run) return;
      // A chunk that no longer exists means the site was rebuilt under this page.
      this.stale.set(e instanceof TypeError && /dynamically imported module|Importing a module script/i.test(e.message));
      this.state.set('error');
    }
  }

  protected retry(): void {
    this.attempt.update((n) => n + 1);
  }

  protected reload(): void {
    location.reload();
  }
}
