//! Rewrites the declarations that describe tax forms and their figures into
//! the core forms, ported from the Go package `taxlisp/expand`.
//!
//! `defform` and `defworksheet` become a `def` over a `withs` of their
//! lines, `deffigures` and `deffigure` become `=`. This engine records no
//! derivation, so a line expands to its expression alone, and `all-of` and
//! `any-of` to `and` and `or`, wherever they are.

use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value;

crate::located_error!(
    /// A declaration that does not have the shape its keyword needs.
    ExpandError
);

/// Expands every declaration in `mf`, in place.
pub fn expand_file(mf: &mut taxlispv1::ModuleFile) -> Result<(), ExpandError> {
    for e in mf.expressions.iter_mut() {
        let out = expand_expr(e)?;
        *e = out;
    }
    Ok(())
}

/// Expands one top-level declaration. Anything else is returned with
/// `all-of` and `any-of` rewritten.
pub fn expand_expr(e: &taxlispv1::Expression) -> Result<taxlispv1::Expression, ExpandError> {
    let out = match head(e) {
        "defform" => form(e, true)?,
        "defworksheet" => form(e, false)?,
        "deffigures" => figures(e)?,
        "deffigure" => figure(e)?,
        _ => e.clone(),
    };
    Ok(logical(out))
}

fn elems(e: &taxlispv1::Expression) -> &[taxlispv1::Expression] {
    match &e.value {
        Some(Value::List(l)) => &l.expressions,
        _ => &[],
    }
}

fn atom(e: &taxlispv1::Expression) -> Option<&Primitive> {
    match &e.value {
        Some(Value::Atom(a)) => a.primitive.as_ref(),
        _ => None,
    }
}

fn head(e: &taxlispv1::Expression) -> &str {
    elems(e).first().map(symbol_name).unwrap_or("")
}

fn symbol_name(e: &taxlispv1::Expression) -> &str {
    match atom(e) {
        Some(Primitive::Symbol(s)) => &s.name,
        _ => "",
    }
}

fn keyword(e: &taxlispv1::Expression) -> &str {
    match atom(e) {
        Some(Primitive::Keyword(k)) => k,
        _ => "",
    }
}

fn line_name(e: &taxlispv1::Expression) -> Option<String> {
    let n = match atom(e) {
        Some(Primitive::IntValue(n)) => n.to_string(),
        Some(Primitive::StringValue(s)) => s.clone(),
        _ => return None,
    };
    Some(format!(
        "line{}",
        n.chars()
            .filter(|c| c.is_ascii_alphanumeric())
            .collect::<String>()
    ))
}

fn sym(name: &str) -> taxlispv1::Expression {
    prim(Primitive::Symbol(Box::new(taxlispv1::Symbol {
        name: name.to_string(),
        ..Default::default()
    })))
}

fn kw(name: &str) -> taxlispv1::Expression {
    prim(Primitive::Keyword(name.to_string()))
}

fn prim(p: Primitive) -> taxlispv1::Expression {
    taxlispv1::Expression {
        value: Some(Value::Atom(Box::new(taxlispv1::Atom {
            primitive: Some(p),
            ..Default::default()
        }))),
        ..Default::default()
    }
}

fn list(expressions: Vec<taxlispv1::Expression>) -> taxlispv1::Expression {
    taxlispv1::Expression {
        value: Some(Value::List(Box::new(taxlispv1::List {
            expressions,
            ..Default::default()
        }))),
        ..Default::default()
    }
}

/// `out` with the syntax, and so the comments, of `e`.
fn with_syntax(mut out: taxlispv1::Expression, e: &taxlispv1::Expression) -> taxlispv1::Expression {
    if let (Some(Value::List(o)), Some(Value::List(l))) = (&mut out.value, &e.value) {
        o.syntax = l.syntax.clone();
    }
    out
}

fn error(e: &taxlispv1::Expression, message: impl Into<String>) -> ExpandError {
    if let Some(Value::List(l)) = &e.value
        && let Some(syn) = l.syntax.as_option()
    {
        return ExpandError::at(
            crate::SourcePos {
                file: syn.file.clone(),
                line: syn.start_line.into(),
                column: syn.start_column.into(),
            },
            message,
        );
    }
    ExpandError::new(message)
}

fn form(e: &taxlispv1::Expression, is_form: bool) -> Result<taxlispv1::Expression, ExpandError> {
    let es = elems(e);
    if es.len() < 4 {
        return Err(error(
            e,
            format!("{} requires a name, parameters and a type", head(e)),
        ));
    }
    let mut out = vec![sym("def"), es[1].clone(), es[2].clone(), es[3].clone()];
    let mut i = 4;
    while i + 1 < es.len() && !keyword(&es[i]).is_empty() {
        out.push(es[i].clone());
        out.push(es[i + 1].clone());
        i += 2;
    }
    let mut bindings = Vec::new();
    let mut fields = Vec::new();
    let mut result = None;
    walk(&es[i..], is_form, &mut bindings, &mut fields, &mut result)?;
    let result = if is_form {
        let mut p = vec![sym("proto"), list(vec![sym("quote"), es[3].clone()])];
        p.extend(fields);
        list(p)
    } else {
        result.ok_or_else(|| error(e, "a worksheet needs a (result expr)"))?
    };
    out.push(list(vec![sym("withs"), list(bindings), result]));
    Ok(with_syntax(list(out), e))
}

fn walk(
    items: &[taxlispv1::Expression],
    is_form: bool,
    bindings: &mut Vec<taxlispv1::Expression>,
    fields: &mut Vec<taxlispv1::Expression>,
    result: &mut Option<taxlispv1::Expression>,
) -> Result<(), ExpandError> {
    for it in items {
        let p = elems(it);
        match head(it) {
            "part" if p.len() >= 2 => walk(&p[2..], is_form, bindings, fields, result)?,
            "line" if p.len() == 4 || p.len() == 6 => {
                let name = line_name(&p[1])
                    .ok_or_else(|| error(it, "a line number is an integer or a string"))?;
                bindings.push(sym(&name));
                bindings.push(p[3].clone());
                if p.len() == 6 {
                    if !is_form || keyword(&p[4]) != "field" {
                        return Err(error(it, "only a form's line can name a :field"));
                    }
                    fields.push(kw(symbol_name(&p[5])));
                    fields.push(sym(&name));
                }
            }
            "attach" if p.len() == 4 => {
                bindings.push(p[1].clone());
                bindings.push(p[3].clone());
            }
            "bind" if p.len() == 3 => {
                bindings.push(p[1].clone());
                bindings.push(p[2].clone());
            }
            "set" if is_form && p.len() == 3 => {
                fields.push(p[1].clone());
                fields.push(p[2].clone());
            }
            "result" if !is_form && p.len() == 2 => *result = Some(p[1].clone()),
            other => {
                return Err(error(
                    it,
                    format!("a form or worksheet cannot hold {other} here"),
                ));
            }
        }
    }
    Ok(())
}

fn figures(e: &taxlispv1::Expression) -> Result<taxlispv1::Expression, ExpandError> {
    let es = elems(e);
    if es.len() < 5 {
        return Err(error(
            e,
            "(deffigures name Type year (from source (field value :at anchor) ...) ...)",
        ));
    }
    let mut args = vec![
        sym("make-struct"),
        es[2].clone(),
        kw("tax_year"),
        es[3].clone(),
    ];
    for group in &es[4..] {
        let g = elems(group);
        if head(group) != "from" || g.len() < 2 {
            return Err(error(
                group,
                "figures are grouped as (from source-id (field value :at anchor) ...)",
            ));
        }
        for f in &g[2..] {
            let fl = elems(f);
            if fl.len() != 4 || keyword(&fl[2]) != "at" {
                return Err(error(f, "a figure is (field value :at anchor)"));
            }
            args.push(kw(symbol_name(&fl[0])));
            args.push(fl[1].clone());
        }
    }
    Ok(with_syntax(
        list(vec![sym("="), es[1].clone(), es[2].clone(), list(args)]),
        e,
    ))
}

fn figure(e: &taxlispv1::Expression) -> Result<taxlispv1::Expression, ExpandError> {
    let es = elems(e);
    if es.len() != 9 || keyword(&es[4]) != "src" || keyword(&es[6]) != "at" {
        return Err(error(
            e,
            "(deffigure name Type year :src source-id :at anchor value)",
        ));
    }
    Ok(with_syntax(
        list(vec![sym("="), es[1].clone(), es[2].clone(), es[8].clone()]),
        e,
    ))
}

/// `e` with every `all-of` and `any-of` rewritten to `and` and `or`.
fn logical(mut e: taxlispv1::Expression) -> taxlispv1::Expression {
    if let Some(Value::List(l)) = &mut e.value {
        for x in l.expressions.iter_mut() {
            *x = logical(std::mem::take(x));
        }
        if let Some(first) = l.expressions.first_mut()
            && let Some(Value::Atom(a)) = &mut first.value
            && let Some(Primitive::Symbol(s)) = &mut a.primitive
        {
            match s.name.as_str() {
                "all-of" => s.name = "and".to_string(),
                "any-of" => s.name = "or".to_string(),
                _ => {}
            }
        }
    }
    e
}
