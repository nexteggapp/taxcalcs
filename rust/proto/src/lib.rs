//! buffa bindings for every schema the taxcalcs crates use: this
//! repository's taxlisp, returns, scenarios and us protos, the taxprotos tax
//! forms, and protodiffgen's config. They are generated from the
//! FileDescriptorSet in descriptors.binpb, which is also here as
//! [`DESCRIPTORS`] for code that reflects over the schemas.

#![allow(missing_docs, clippy::all)]

include!(concat!(env!("OUT_DIR"), "/_include.rs"));

/// The serialized FileDescriptorSet the bindings were generated from: every
/// file above and the files it imports.
pub const DESCRIPTORS: &[u8] = include_bytes!("../descriptors.binpb");

/// `nexteggapp.taxcalcs.returns.v1`, Go's `returnsv1`.
pub use nexteggapp::taxcalcs::returns::v1 as returnsv1;
/// `nexteggapp.taxcalcs.scenarios.v1`, Go's `scenariosv1`.
pub use nexteggapp::taxcalcs::scenarios::v1 as scenariosv1;
/// `nexteggapp.taxcalcs.taxlisp.v1`, Go's `taxlispv1`.
pub use nexteggapp::taxcalcs::taxlisp::v1 as taxlispv1;
/// `nexteggapp.taxcalcs.us.v1`, Go's `usv1`.
pub use nexteggapp::taxcalcs::us::v1 as usv1;
/// `nexteggapp.taxprotos.common.v1`, Go's `commonv1`.
pub use nexteggapp::taxprotos::common::v1 as commonv1;
/// `nexteggapp.taxprotos.facts.v1`, Go's `factsv1`.
pub use nexteggapp::taxprotos::facts::v1 as factsv1;
/// `nexteggapp.taxprotos.us.federal.v1`: every federal form. Go has one
/// package per form directory (`form1040`, `formw2`, ...); the proto package
/// is one.
pub use nexteggapp::taxprotos::us::federal::v1 as federalv1;
/// `nexteggapp.taxprotos.us.state.al.v1`: Alabama's forms.
pub use nexteggapp::taxprotos::us::state::al::v1 as alv1;
/// `nexteggapp.taxprotos.us.state.ca.v1`: California's forms.
pub use nexteggapp::taxprotos::us::state::ca::v1 as cav1;
/// `nexteggapp.taxprotos.us.state.co.v1`: Colorado's forms.
pub use nexteggapp::taxprotos::us::state::co::v1 as cov1;
/// `nexteggapp.taxprotos.us.state.il.v1`: Illinois's forms.
pub use nexteggapp::taxprotos::us::state::il::v1 as ilv1;
/// `nexteggapp.taxprotos.us.state.me.v1`: Maine's forms.
pub use nexteggapp::taxprotos::us::state::me::v1 as mev1;
/// `nexteggapp.taxprotos.us.state.nc.v1`: North Carolina's forms.
pub use nexteggapp::taxprotos::us::state::nc::v1 as ncv1;
/// `nexteggapp.taxprotos.us.state.or.v1`: Oregon's forms.
pub use nexteggapp::taxprotos::us::state::or::v1 as orv1;

/// `nexteggapp.taxprotos.us.state.ar.v1`: Arkansas's forms.
pub use nexteggapp::taxprotos::us::state::ar::v1 as arv1;
/// `nexteggapp.taxprotos.us.state.az.v1`: Arizona's forms.
pub use nexteggapp::taxprotos::us::state::az::v1 as azv1;
/// `nexteggapp.taxprotos.us.state.dc.v1`: the District of Columbia's forms.
pub use nexteggapp::taxprotos::us::state::dc::v1 as dcv1;
/// `nexteggapp.taxprotos.us.state.ga.v1`: Georgia's forms.
pub use nexteggapp::taxprotos::us::state::ga::v1 as gav1;
/// `nexteggapp.taxprotos.us.state.ia.v1`: Iowa's forms.
pub use nexteggapp::taxprotos::us::state::ia::v1 as iav1;
/// `nexteggapp.taxprotos.us.state.id.v1`: Idaho's forms.
pub use nexteggapp::taxprotos::us::state::id::v1 as idv1;
/// `nexteggapp.taxprotos.us.state.in.v1`: Indiana's forms.
pub use nexteggapp::taxprotos::us::state::r#in::v1 as inv1;
/// `nexteggapp.taxprotos.us.state.ks.v1`: Kansas's forms.
pub use nexteggapp::taxprotos::us::state::ks::v1 as ksv1;
/// `nexteggapp.taxprotos.us.state.ky.v1`: Kentucky's forms.
pub use nexteggapp::taxprotos::us::state::ky::v1 as kyv1;
/// `nexteggapp.taxprotos.us.state.la.v1`: Louisiana's forms.
pub use nexteggapp::taxprotos::us::state::la::v1 as lav1;
/// `nexteggapp.taxprotos.us.state.mi.v1`: Michigan's forms.
pub use nexteggapp::taxprotos::us::state::mi::v1 as miv1;
/// `nexteggapp.taxprotos.us.state.mo.v1`: Missouri's forms.
pub use nexteggapp::taxprotos::us::state::mo::v1 as mov1;
/// `nexteggapp.taxprotos.us.state.ms.v1`: Mississippi's forms.
pub use nexteggapp::taxprotos::us::state::ms::v1 as msv1;
/// `nexteggapp.taxprotos.us.state.mt.v1`: Montana's forms.
pub use nexteggapp::taxprotos::us::state::mt::v1 as mtv1;
/// `nexteggapp.taxprotos.us.state.nd.v1`: North Dakota's forms.
pub use nexteggapp::taxprotos::us::state::nd::v1 as ndv1;
/// `nexteggapp.taxprotos.us.state.ne.v1`: Nebraska's forms.
pub use nexteggapp::taxprotos::us::state::ne::v1 as nev1;
/// `nexteggapp.taxprotos.us.state.nm.v1`: New Mexico's forms.
pub use nexteggapp::taxprotos::us::state::nm::v1 as nmv1;
/// `nexteggapp.taxprotos.us.state.ok.v1`: Oklahoma's forms.
pub use nexteggapp::taxprotos::us::state::ok::v1 as okv1;
/// `nexteggapp.taxprotos.us.state.pa.v1`: Pennsylvania's forms.
pub use nexteggapp::taxprotos::us::state::pa::v1 as pav1;
/// `nexteggapp.taxprotos.us.state.sc.v1`: South Carolina's forms.
pub use nexteggapp::taxprotos::us::state::sc::v1 as scv1;
/// `nexteggapp.taxprotos.us.state.ut.v1`: Utah's forms.
pub use nexteggapp::taxprotos::us::state::ut::v1 as utv1;
/// `nexteggapp.taxprotos.us.state.va.v1`: Virginia's forms.
pub use nexteggapp::taxprotos::us::state::va::v1 as vav1;
/// `nexteggapp.taxprotos.us.state.wa.v1`: Washington's capital gains excise
/// tax return.
pub use nexteggapp::taxprotos::us::state::wa::v1 as wav1;
/// `nexteggapp.taxprotos.us.state.wv.v1`: West Virginia's forms.
pub use nexteggapp::taxprotos::us::state::wv::v1 as wvv1;
/// `nexteggapp.taxprotos.us.state.hi.v1`: Hawaii's forms.
pub use nexteggapp::taxprotos::us::state::hi::v1 as hiv1;
/// `nexteggapp.taxprotos.us.state.vt.v1`: Vermont's forms.
pub use nexteggapp::taxprotos::us::state::vt::v1 as vtv1;
/// `nexteggapp.taxprotos.us.state.de.v1`: Delaware's forms.
pub use nexteggapp::taxprotos::us::state::de::v1 as dev1;
/// `nexteggapp.taxprotos.us.state.ct.v1`: Connecticut's forms.
pub use nexteggapp::taxprotos::us::state::ct::v1 as ctv1;
/// `nextgui.protodiffgen.v1`, Go's `configv1`.
pub use nextgui::protodiffgen::v1 as configv1;
