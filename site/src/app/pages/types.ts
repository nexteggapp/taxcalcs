import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import data from '../../model/types.json';
import { Crumbs } from '../crumbs';
import { linker } from '../glossary';
import { Links } from '../links';
import { inline } from '../markdown';
import { model, ProtoType, slug } from '../model';
import { pageTitle } from '../page-title';

const types = data as unknown as ProtoType[];
const bySlug = new Map(types.map((t) => [t.slug, t]));

/** A package's name as the pages show it: returns.v1, taxprotos.common.v1. */
function pkg(name: string): string {
  return name.replace(/^nexteggapp\.taxcalcs\./, '').replace(/^nexteggapp\./, '');
}

/** One protobuf message or enum, from its .proto file. */
@Component({
  selector: 'app-type',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs, Links],
  template: `
    @if (type(); as t) {
      <app-crumbs [items]="[{ label: 'Home', link: ['/'] }, { label: 'Types', link: ['/types'] }, { label: t.short }]" />
      <p class="eyebrow">{{ t.kind === 'enum' ? 'Enum' : 'Message' }} · {{ pkg(t.package) }}</p>
      <h1 class="mono type-name">{{ t.short }}</h1>
      @if (t.doc) {
        <div class="doc" appLinks [innerHTML]="md(t.doc)"></div>
      }
      <p class="muted small">
        <code>{{ t.name }}</code>, in <code>{{ t.file }}</code>
      </p>

      @if (t.fields?.length) {
        <h2>Fields</h2>
        <div class="table-wrap">
          <table class="fields">
            <thead>
              <tr><th>Field</th><th>Type</th><th>What it is</th></tr>
            </thead>
            <tbody>
              @for (f of t.fields; track f.name) {
                <tr [id]="f.name">
                  <td>
                    <code>{{ f.name }}</code> <span class="muted small">= {{ f.number }}</span>
                    @if (f.oneof) {
                      <div class="muted small">one of {{ f.oneof }}</div>
                    }
                  </td>
                  <td>
                    @if (f.repeated) {
                      <span class="muted small">repeated </span>
                    }
                    @if (f.ref) {
                      <a [routerLink]="['/types', f.ref]"><code>{{ f.type }}</code></a>
                    } @else {
                      <code>{{ f.type }}</code>
                    }
                  </td>
                  <td>
                    @if (f.doc) {
                      <div appLinks [innerHTML]="md(f.doc)"></div>
                    }
                    @if (f.readBy?.length) {
                      <div class="read-by small">
                        Read by
                        @for (r of f.readBy; track r; let last = $last) {
                          <a [routerLink]="['/rules', ruleSlug(r)]"><code>{{ r }}</code></a>{{ last ? '' : ', ' }}
                        }
                      </div>
                    }
                  </td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }

      @if (t.values?.length) {
        <h2>Values</h2>
        <div class="table-wrap">
          <table class="fields">
            <thead>
              <tr><th>Value</th><th>Number</th><th>What it means</th></tr>
            </thead>
            <tbody>
              @for (v of t.values; track v.name) {
                <tr>
                  <td><code>{{ v.name }}</code></td>
                  <td>{{ v.number }}</td>
                  <td appLinks [innerHTML]="md(v.doc ?? '')"></td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }

      @if (t.usedBy?.length) {
        <h2>Used by</h2>
        <p class="type-list">
          @for (u of t.usedBy; track u) {
            <a [routerLink]="['/types', u]"><code>{{ short(u) }}</code></a>
          }
        </p>
      }
    } @else {
      <h1>No such type</h1>
      <p><a routerLink="/types">All types</a></p>
    }
  `,
})
export class TypePage {
  readonly slug = input.required<string>();
  private readonly sanitizer = inject(DomSanitizer);
  protected readonly type = computed(() => bySlug.get(this.slug()));
  protected readonly pkg = pkg;
  protected readonly ruleSlug = slug;

  constructor() {
    pageTitle(() => this.type()?.short);
  }

  protected md(s: string) {
    return this.sanitizer.bypassSecurityTrustHtml(inline(s.replace(/\s*\n\s*/g, ' '), linker(false)));
  }
  protected short(s: string): string {
    return bySlug.get(s)?.short ?? s;
  }
}

/** Every protobuf type of the return contract, by package. */
@Component({
  selector: 'app-types-index',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="[{ label: 'Home', link: ['/'] }, { label: 'Types' }]" />
    <h1>Types</h1>
    <p class="doc">
      The protocol buffer messages and enums taxcalcs takes and returns, starting from ReturnInputs and
      ReturnOutputs, with every type they use. Each page is generated from the type's .proto file and its comments,
      and lists the rules that read each field.
    </p>
    <input class="filter" type="search" placeholder="Filter by name" [value]="q()" (input)="q.set($any($event.target).value)" aria-label="Filter types" />
    @for (g of groups(); track g.pkg) {
      <h2 class="mono small-h">{{ g.pkg }}</h2>
      <ul class="az">
        @for (t of g.types; track t.slug) {
          <li>
            <a [routerLink]="['/types', t.slug]"><code>{{ t.short }}</code></a>
            @if (t.kind === 'enum') {
              <span class="badge small">enum</span>
            }
            @if (t.summary) {
              <div class="muted small">{{ t.summary }}</div>
            }
          </li>
        }
      </ul>
    }
  `,
})
export class TypesIndex {
  protected readonly q = signal('');
  protected readonly groups = computed(() => {
    const q = this.q().toLowerCase().trim();
    const shown = model.typeIndex.filter((t) => !q || t.slug.toLowerCase().includes(q));
    const out = new Map<string, typeof shown>();
    for (const t of shown) {
      const p = t.slug.slice(0, t.slug.length - t.short.length - 1);
      out.set(p, [...(out.get(p) ?? []), t]);
    }
    return [...out].map(([p, ts]) => ({ pkg: p, types: ts }));
  });

  constructor() {
    pageTitle(() => 'Types');
  }
}
