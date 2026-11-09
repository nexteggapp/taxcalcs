import { RenderMode, ServerRoute } from '@angular/ssr';
import { citing, model, slug } from './model';
import { states } from './jurisdictions';

// Every page is prerendered to static HTML, one per source, anchor, rule,
// figure set, cited section and form.
export const serverRoutes: ServerRoute[] = [
  { path: '', renderMode: RenderMode.Prerender },
  {
    path: 'sources/:id',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.sources.map((s) => ({ id: s.id })),
  },
  {
    path: 'sources/:id/:anchor',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.sources.flatMap((s) => (s.anchors ?? []).map((a) => ({ id: s.id, anchor: a.id }))),
  },
  {
    path: 'rules/:name',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.rules.map((r) => ({ name: slug(r.name) })),
  },
  {
    path: 'figures/:name',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.figureSets.map((f) => ({ name: f.name })),
  },
  {
    path: 'law/:source/:section',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () =>
      [...citing.keys()].map((k) => {
        const [source, section] = k.split('#');
        return { source, section };
      }),
  },
  {
    path: 'walkthrough/:form',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.rules.filter((r) => r.kind === 'form').map((r) => ({ form: r.name })),
  },
  {
    path: ':year/:code',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => [{ code: 'us' }, ...states].map((s) => ({ year: String(model.year), code: s.code.toLowerCase() })),
  },
  { path: 'taxlisp', renderMode: RenderMode.Prerender },
  { path: 'glossary', renderMode: RenderMode.Prerender },
  { path: 'estimates', renderMode: RenderMode.Prerender },
  { path: 'types', renderMode: RenderMode.Prerender },
  {
    path: 'types/:slug',
    renderMode: RenderMode.Prerender,
    getPrerenderParams: async () => model.typeIndex.map((t) => ({ slug: t.slug })),
  },
  { path: '404', renderMode: RenderMode.Prerender },
  { path: '**', renderMode: RenderMode.Prerender },
];
