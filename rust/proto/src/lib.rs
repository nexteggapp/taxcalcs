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
/// `nexteggapp.taxprotos.us.state.az.v1`: Arizona's forms.
pub use nexteggapp::taxprotos::us::state::az::v1 as azv1;
/// `nexteggapp.taxprotos.us.state.ca.v1`: California's forms.
pub use nexteggapp::taxprotos::us::state::ca::v1 as cav1;
/// `nexteggapp.taxprotos.us.state.co.v1`: Colorado's forms.
pub use nexteggapp::taxprotos::us::state::co::v1 as cov1;
/// `nexteggapp.taxprotos.us.state.il.v1`: Illinois's forms.
pub use nexteggapp::taxprotos::us::state::il::v1 as ilv1;
/// `nexteggapp.taxprotos.us.state.nc.v1`: North Carolina's forms.
pub use nexteggapp::taxprotos::us::state::nc::v1 as ncv1;
/// `nexteggapp.taxprotos.us.state.or.v1`: Oregon's forms.
pub use nexteggapp::taxprotos::us::state::or::v1 as orv1;
/// `nexteggapp.taxprotos.us.state.wa.v1`: Washington's capital gains excise
/// tax return.
pub use nexteggapp::taxprotos::us::state::wa::v1 as wav1;
/// `nexteggapp.taxprotos.us.state.ga.v1`: Georgia's forms.
pub use nexteggapp::taxprotos::us::state::ga::v1 as gav1;
/// `nexteggapp.taxprotos.us.state.id.v1`: Idaho's forms.
pub use nexteggapp::taxprotos::us::state::id::v1 as idv1;
/// `nexteggapp.taxprotos.us.state.ut.v1`: Utah's forms.
pub use nexteggapp::taxprotos::us::state::ut::v1 as utv1;
/// `nexteggapp.taxprotos.us.state.mi.v1`: Michigan's forms.
pub use nexteggapp::taxprotos::us::state::mi::v1 as miv1;
/// `nexteggapp.taxprotos.us.state.pa.v1`: Pennsylvania's forms.
pub use nexteggapp::taxprotos::us::state::pa::v1 as pav1;
/// `nexteggapp.taxprotos.us.state.ky.v1`: Kentucky's forms.
pub use nexteggapp::taxprotos::us::state::ky::v1 as kyv1;
/// `nexteggapp.taxprotos.us.state.in.v1`: Indiana's forms.
pub use nexteggapp::taxprotos::us::state::r#in::v1 as inv1;
/// `nexteggapp.taxprotos.us.state.ia.v1`: Iowa's forms.
pub use nexteggapp::taxprotos::us::state::ia::v1 as iav1;
/// `nextgui.protodiffgen.v1`, Go's `configv1`.
pub use nextgui::protodiffgen::v1 as configv1;
