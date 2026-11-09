import { Routes } from '@angular/router';
import { Home } from './pages/home';
import { JurisdictionPage } from './pages/jurisdiction';
import { SourcePage } from './pages/source';
import { RulePage } from './pages/rule';
import { FiguresPage } from './pages/figures';
import { LawPage } from './pages/law';
import { Walkthrough } from './pages/walkthrough';
import { RulesIndex, SourcesIndex } from './pages/indexes';
import { NotFound } from './pages/not-found';

export const routes: Routes = [
  { path: '', component: Home, title: 'taxcalcs: an open-source tax calculation library' },
  { path: 'sources', component: SourcesIndex },
  { path: 'sources/:id', component: SourcePage },
  { path: 'sources/:id/:anchor', component: SourcePage },
  { path: 'rules', component: RulesIndex },
  { path: 'rules/:name', component: RulePage },
  { path: 'figures/:name', component: FiguresPage },
  { path: 'law/:source/:section', component: LawPage },
  { path: 'walkthrough/:form', component: Walkthrough },
  { path: 'taxlisp', loadComponent: () => import('./pages/taxlisp').then((m) => m.TaxlispPage) },
  { path: 'glossary', loadComponent: () => import('./pages/glossary').then((m) => m.GlossaryPage) },
  { path: 'estimates', loadComponent: () => import('./pages/estimates').then((m) => m.EstimatesPage) },
  { path: 'types', loadComponent: () => import('./pages/types').then((m) => m.TypesIndex) },
  { path: 'types/:slug', loadComponent: () => import('./pages/types').then((m) => m.TypePage) },
  { path: ':year/:code', component: JurisdictionPage },
  { path: '404', component: NotFound },
  { path: '**', component: NotFound },
];
