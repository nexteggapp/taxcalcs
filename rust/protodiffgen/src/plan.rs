//! What the runtime knows about each patch body, read from its descriptor
//! and options: plan.go.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, MutexGuard};

use buffa_descriptor::{
    DescriptorPool, FieldDescriptor, FieldKind, MessageDescriptor, MessageIndex, ScalarType,
    SingularKind,
};

use crate::Error;
use crate::dynamic::{
    by_name, field_full_name, go_kind, has_message, is_list, is_map, message_type,
};
use crate::schema::{self, CLEAR_NUMBER};

/// How a field of a patch body changes its source field.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Kind {
    /// A singular scalar or enum, set.
    Scalar,
    /// A singular message, set whole.
    Atomic,
    /// A singular message, patched.
    Patch,
    /// A keyed list or a map.
    Keyed,
    /// An indexed list.
    Indexed,
    /// A list set whole.
    Replace,
}

/// A field of a message of the diff's pool, by the message and the field's
/// position in it, so it is found again without a search.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct FieldRef {
    msg: MessageIndex,
    pos: usize,
}

impl FieldRef {
    /// The field's descriptor.
    pub(crate) fn fd<'p>(&self, pool: &'p DescriptorPool) -> &'p FieldDescriptor {
        &pool.message(self.msg).fields()[self.pos]
    }
}

/// What the runtime knows about one patch body.
#[derive(Debug)]
pub(crate) struct Plan {
    pub(crate) body: MessageIndex,
    pub(crate) source: String,
    pub(crate) fields: Vec<FPlan>,
    pub(crate) by_num: HashMap<u32, usize>,
    pub(crate) clear: FieldRef,
}

/// One source field of a patch body.
#[derive(Debug)]
pub(crate) struct FPlan {
    pub(crate) kind: Kind,
    pub(crate) num: u32,
    /// The source field.
    pub(crate) name: String,
    pub(crate) diff: FieldRef,
    /// The item's or message's patch body; None when set whole.
    pub(crate) sub: Option<MessageIndex>,
    pub(crate) wrap: Option<MessageIndex>,
    /// The op oneof's position in the wrapper.
    pub(crate) op: Option<usize>,
    /// The wrapper's key.
    pub(crate) key: Option<FieldRef>,
    /// The key's name in the item.
    pub(crate) key_src: String,
    pub(crate) index: Option<FieldRef>,
    pub(crate) add: Option<FieldRef>,
    pub(crate) remove: Option<FieldRef>,
    pub(crate) patch: Option<FieldRef>,
    pub(crate) set: Option<FieldRef>,
    pub(crate) insert: Option<FieldRef>,
    /// Inserted.items, or a Replace wrapper's list.
    pub(crate) items: Option<FieldRef>,
    /// In the parent body; ORDERED keyed lists only.
    pub(crate) order: Option<FieldRef>,
}

/// The plans of one pool, each by its patch body, with the pool. A plan's
/// sub-plans are found here by their bodies.
#[derive(Clone, Debug)]
pub(crate) struct Plans {
    pub(crate) pool: Arc<DescriptorPool>,
    map: Arc<HashMap<MessageIndex, Arc<Plan>>>,
}

impl Plans {
    /// The plan for body, which a plan found here refers to.
    pub(crate) fn get(&self, body: MessageIndex) -> &Plan {
        // Every plan's sub-plans are built and stored with it.
        &self.map[&body]
    }

    /// The plan for a sub-plan's body, if any.
    pub(crate) fn sub(&self, sub: Option<MessageIndex>) -> Option<&Plan> {
        sub.map(|s| self.get(s))
    }
}

/// The plans built for one pool, and the bodies that are not patch bodies.
struct PoolPlans {
    pool: Arc<DescriptorPool>,
    plans: Arc<HashMap<MessageIndex, Arc<Plan>>>,
    errs: HashMap<MessageIndex, Error>,
    rules: HashMap<String, Rules>,
}

#[derive(Default)]
struct Cache {
    /// By the pool's address; each entry holds its pool, so the address is
    /// not reused while it is here.
    pools: HashMap<usize, PoolPlans>,
    /// Which plans have been checked against which target types: the plan's
    /// pool and body, and the target's pool and type. Each holds the
    /// target's pool, so its address is not reused while it is here.
    bound: HashMap<(usize, MessageIndex, usize, MessageIndex), Arc<DescriptorPool>>,
}

static CACHE: Mutex<Option<Cache>> = Mutex::new(None);

fn cache() -> MutexGuard<'static, Option<Cache>> {
    CACHE.lock().unwrap_or_else(|e| e.into_inner())
}

fn pool_key(pool: &Arc<DescriptorPool>) -> usize {
    Arc::as_ptr(pool) as usize
}

/// The plan for a patch body, and the plans of its pool.
pub(crate) fn plan_for(
    pool: &Arc<DescriptorPool>,
    md: MessageIndex,
) -> Result<(Plans, Arc<Plan>), Error> {
    let mut guard = cache();
    let c = guard.get_or_insert_with(Cache::default);
    let pp = c.pools.entry(pool_key(pool)).or_insert_with(|| PoolPlans {
        pool: Arc::clone(pool),
        plans: Arc::new(HashMap::new()),
        errs: HashMap::new(),
        rules: HashMap::new(),
    });
    plan_in(pp, md)
}

fn plan_in(pp: &mut PoolPlans, md: MessageIndex) -> Result<(Plans, Arc<Plan>), Error> {
    let found = |pp: &PoolPlans| {
        pp.plans.get(&md).map(|p| {
            (
                Plans {
                    pool: Arc::clone(&pp.pool),
                    map: Arc::clone(&pp.plans),
                },
                Arc::clone(p),
            )
        })
    };
    if let Some(out) = found(pp) {
        return Ok(out);
    }
    if let Some(err) = pp.errs.get(&md) {
        return Err(err.clone());
    }
    let mut building: HashMap<MessageIndex, Option<Plan>> = HashMap::new();
    if let Err(err) = build(&pp.pool, &pp.plans, md, &mut building) {
        pp.errs.insert(md, err.clone());
        return Err(err);
    }
    let mut plans = (*pp.plans).clone();
    for (d, bp) in building {
        if let Some(bp) = bp {
            plans.insert(d, Arc::new(bp));
        }
    }
    pp.plans = Arc::new(plans);
    found(pp).ok_or_else(|| {
        Error::Schema(format!(
            "{} is not a protodiffgen patch body",
            pp.pool.message(md).full_name()
        ))
    })
}

fn build(
    pool: &DescriptorPool,
    plans: &HashMap<MessageIndex, Arc<Plan>>,
    md: MessageIndex,
    building: &mut HashMap<MessageIndex, Option<Plan>>,
) -> Result<(), Error> {
    if plans.contains_key(&md) || building.contains_key(&md) {
        return Ok(());
    }
    let d = pool.message(md);
    let source = match schema::message_change(d) {
        Some((source, schema::Kind::Patch)) => source,
        _ => {
            return Err(Error::Schema(format!(
                "{} is not a protodiffgen patch body",
                d.full_name()
            )));
        }
    };
    building.insert(md, None);
    let mut p = Plan {
        body: md,
        source,
        fields: Vec::new(),
        by_num: HashMap::new(),
        clear: FieldRef { msg: md, pos: 0 },
    };
    let mut has_clear = false;
    let mut orders: Vec<(String, FieldRef)> = Vec::new();
    for (pos, fd) in d.fields().iter().enumerate() {
        let here = FieldRef { msg: md, pos };
        if fd.number() == CLEAR_NUMBER {
            if fd.kind() != FieldKind::List(SingularKind::Scalar(ScalarType::Int32)) {
                return Err(Error::Schema(format!(
                    "{}: the clear field is not repeated int32",
                    field_full_name(d, fd)
                )));
            }
            p.clear = here;
            has_clear = true;
            continue;
        }
        let Some(src) = schema::field_source_of(fd) else {
            return Err(Error::Schema(format!(
                "{} has no protodiffgen field option",
                field_full_name(d, fd)
            )));
        };
        if src.order {
            orders.push((src.source, here));
            continue;
        }
        let mut fp = FPlan {
            kind: Kind::Scalar,
            num: fd.number(),
            name: src.source,
            diff: here,
            sub: None,
            wrap: None,
            op: None,
            key: None,
            key_src: String::new(),
            index: None,
            add: None,
            remove: None,
            patch: None,
            set: None,
            insert: None,
            items: None,
            order: None,
        };
        classify(&mut fp, pool, plans, building)?;
        p.by_num.insert(fp.num, p.fields.len());
        p.fields.push(fp);
    }
    if !has_clear {
        return Err(Error::Schema(format!(
            "{} has no clear field",
            d.full_name()
        )));
    }
    for (name, fd) in orders {
        let mut found = false;
        for fp in &mut p.fields {
            if fp.name == name && fp.kind == Kind::Keyed {
                fp.order = Some(fd);
                found = true;
            }
        }
        if !found {
            return Err(Error::Schema(format!(
                "{}: order field for unknown list {}",
                field_full_name(d, fd.fd(pool)),
                name
            )));
        }
    }
    building.insert(md, Some(p));
    Ok(())
}

fn classify(
    fp: &mut FPlan,
    pool: &DescriptorPool,
    plans: &HashMap<MessageIndex, Arc<Plan>>,
    building: &mut HashMap<MessageIndex, Option<Plan>>,
) -> Result<(), Error> {
    let body = pool.message(fp.diff.msg);
    let fd = fp.diff.fd(pool);
    if !has_message(fd) {
        if is_list(fd) {
            return Err(Error::Schema(format!(
                "{}: unexpected repeated scalar",
                field_full_name(body, fd)
            )));
        }
        fp.kind = Kind::Scalar;
        return Ok(());
    }
    let Some(wi) = message_type(fd) else {
        // A map field: its entry type carries no change option.
        fp.kind = Kind::Atomic;
        return Ok(());
    };
    let wm = pool.message(wi);
    let k = schema::message_change(wm).map_or(schema::Kind::Unspecified, |(_, k)| k);
    let list = is_list(fd);
    match (list, k) {
        (true, schema::Kind::ListChange) => fp.kind = Kind::Keyed,
        (true, schema::Kind::ListEdit) => fp.kind = Kind::Indexed,
        (false, schema::Kind::Replace) => fp.kind = Kind::Replace,
        (false, schema::Kind::Patch) => {
            fp.kind = Kind::Patch;
            build(pool, plans, wi, building)?;
            fp.sub = Some(wi);
            return Ok(());
        }
        (false, schema::Kind::Unspecified) => {
            fp.kind = Kind::Atomic;
            return Ok(());
        }
        _ => {
            return Err(Error::Schema(format!(
                "{}: unexpected type {}",
                field_full_name(body, fd),
                wm.full_name()
            )));
        }
    }
    fp.wrap = Some(wi);
    let at = |pos: usize| FieldRef { msg: wi, pos };
    let named = |name: &str| by_name(wm, name).map(at);
    if fp.kind == Kind::Replace {
        fp.items = wm.fields().iter().position(|f| f.number() == 1).map(at);
        if !fp.items.is_some_and(|f| is_list(f.fd(pool))) {
            return Err(Error::Schema(format!(
                "{} has no list field",
                wm.full_name()
            )));
        }
        return Ok(());
    }
    fp.op = wm.oneofs().iter().position(|o| o.name() == "op");
    if fp.op.is_none() {
        return Err(Error::Schema(format!("{} has no op oneof", wm.full_name())));
    }
    fp.remove = named("remove");
    fp.patch = named("patch");
    for (pos, f) in wm.fields().iter().enumerate() {
        if let Some(s) = schema::field_source_of(f) {
            if s.key {
                fp.key = Some(at(pos));
                fp.key_src = last_name(&s.source).to_string();
            }
            if s.index {
                fp.index = Some(at(pos));
            }
        }
    }
    if fp.kind == Kind::Keyed {
        fp.add = named("add");
        fp.set = named("set");
        if fp.key.is_none()
            || fp.remove.is_none()
            || (fp.set.is_none() && (fp.add.is_none() || fp.patch.is_none()))
        {
            return Err(Error::Schema(format!(
                "{} is not a change wrapper",
                wm.full_name()
            )));
        }
    } else {
        fp.insert = named("insert_before");
        let inserted = fp.insert.and_then(|f| message_type(f.fd(pool)));
        let (Some(_), Some(_), Some(_), Some(ins)) = (fp.index, fp.remove, fp.patch, inserted)
        else {
            return Err(Error::Schema(format!(
                "{} is not an indexed edit",
                wm.full_name()
            )));
        };
        let im = pool.message(ins);
        fp.items = by_name(im, "items").map(|pos| FieldRef { msg: ins, pos });
        if fp.items.is_none() {
            return Err(Error::Schema(format!("{} has no items", im.full_name())));
        }
    }
    if let Some(pi) = fp.patch.and_then(|f| message_type(f.fd(pool)))
        && let Some((_, schema::Kind::Patch)) = schema::message_change(pool.message(pi))
    {
        build(pool, plans, pi, building)?;
        fp.sub = Some(pi);
    }
    Ok(())
}

/// The last part of a full name.
fn last_name(full: &str) -> &str {
    full.rsplit('.').next().unwrap_or(full)
}

impl FPlan {
    /// Whether d changes the field: sets it, or for an ORDERED list, changes
    /// its items or its order.
    pub(crate) fn present(
        &self,
        pool: &DescriptorPool,
        d: &buffa_descriptor::DynamicMessage,
    ) -> bool {
        use buffa_descriptor::ReflectMessage as _;
        d.has(self.diff.fd(pool)) || self.order.is_some_and(|o| d.has(o.fd(pool)))
    }
}

impl Plan {
    /// Checks that the plan is a patch body for messages of type md, of the
    /// pool tpool.
    pub(crate) fn bind(
        &self,
        ps: &Plans,
        tpool: &Arc<DescriptorPool>,
        tmd: MessageIndex,
    ) -> Result<(), Error> {
        let key = (pool_key(&ps.pool), self.body, pool_key(tpool), tmd);
        if let Some(c) = cache().as_ref()
            && c.bound.contains_key(&key)
        {
            return Ok(());
        }
        let pool = &*ps.pool;
        let body = pool.message(self.body);
        let md = tpool.message(tmd);
        if md.full_name() != self.source {
            return Err(Error::Schema(format!(
                "{} diffs {}, not {}",
                body.full_name(),
                self.source,
                md.full_name()
            )));
        }
        for fp in &self.fields {
            let tfd = match md.field(fp.num) {
                Some(tfd) if field_full_name(md, tfd) == fp.name => tfd,
                _ => {
                    return Err(Error::Schema(format!(
                        "{}: {} has no field {} numbered {}",
                        body.full_name(),
                        md.full_name(),
                        fp.name,
                        fp.num
                    )));
                }
            };
            let dfd = fp.diff.fd(pool);
            let ok = match fp.kind {
                Kind::Scalar => !is_list(tfd) && !has_message(tfd) && go_kind(tfd) == go_kind(dfd),
                Kind::Atomic | Kind::Patch => !is_list(tfd) && has_message(tfd),
                Kind::Keyed => {
                    is_map(tfd)
                        || (is_list(tfd)
                            && message_type(tfd)
                                .is_some_and(|i| by_name(tpool.message(i), &fp.key_src).is_some()))
                }
                Kind::Indexed | Kind::Replace => is_list(tfd) && !is_map(tfd),
            };
            if !ok {
                return Err(Error::Schema(format!(
                    "{}: field {} doesn't match {}",
                    body.full_name(),
                    dfd.name(),
                    field_full_name(md, tfd)
                )));
            }
        }
        cache()
            .get_or_insert_with(Cache::default)
            .bound
            .insert(key, Arc::clone(tpool));
        Ok(())
    }

    /// The patch body's descriptor.
    pub(crate) fn body_desc<'p>(&self, pool: &'p DescriptorPool) -> &'p MessageDescriptor {
        pool.message(self.body)
    }
}

/// Maps each source type to its patch body's plan, for comparing messages
/// under the list rules of one generated file.
#[derive(Clone, Debug)]
pub(crate) struct Rules {
    pub(crate) plans: Option<Plans>,
    by_source: Arc<HashMap<String, MessageIndex>>,
}

impl Rules {
    /// No rules: every list compares element by element.
    pub(crate) fn none() -> Rules {
        Rules {
            plans: None,
            by_source: Arc::new(HashMap::new()),
        }
    }

    /// The plan whose source is the type named name.
    pub(crate) fn plan(&self, name: &str) -> Option<&Plan> {
        let body = self.by_source.get(name)?;
        self.plans.as_ref().map(|ps| ps.get(*body))
    }
}

/// The rules of the file that declares md.
pub(crate) fn rules_for(pool: &Arc<DescriptorPool>, md: MessageIndex) -> Rules {
    let name = pool.message(md).full_name().to_string();
    let Some(file) = pool.file_containing_symbol(&name) else {
        return Rules::none();
    };
    let file_name = file.name.clone().unwrap_or_default();
    let prefix = match file.package.as_deref() {
        Some(p) if !p.is_empty() => format!("{p}."),
        _ => String::new(),
    };
    let names: Vec<String> = file
        .message_type
        .iter()
        .map(|m| format!("{prefix}{}", m.name.as_deref().unwrap_or_default()))
        .collect();
    let mut guard = cache();
    let c = guard.get_or_insert_with(Cache::default);
    let pp = c.pools.entry(pool_key(pool)).or_insert_with(|| PoolPlans {
        pool: Arc::clone(pool),
        plans: Arc::new(HashMap::new()),
        errs: HashMap::new(),
        rules: HashMap::new(),
    });
    if let Some(r) = pp.rules.get(&file_name) {
        return r.clone();
    }
    let mut by_source = HashMap::new();
    let mut last = None;
    for n in names {
        let Some(idx) = pool.message_index(&n) else {
            continue;
        };
        if let Ok((ps, p)) = plan_in(pp, idx) {
            by_source.insert(p.source.clone(), idx);
            last = Some(ps);
        }
    }
    // The last snapshot holds every plan built before it.
    let r = Rules {
        plans: last,
        by_source: Arc::new(by_source),
    };
    pp.rules.insert(file_name, r.clone());
    r
}
