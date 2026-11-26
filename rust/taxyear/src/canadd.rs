//! Each model's can_add: canadd.go.
//!
//! Each model's can_add, as the employer and tax paid models fill their
//! own: each part kind that can be added now, with its defaults. Nothing
//! is offered once the year's form is in. A total or an end is offered
//! once, while the model has none, with the totals filled from the
//! projection; a dated part starts the day after the evidence.

use std::collections::HashSet;

use buffa::{Message, MessageField, MessageName, ProtoBox};
use buffa_descriptor::Value;
use money::USCents;
use proto::commonv1;
use proto::scenariosv1::{
    self, __buffa::oneof, AccountModel, AccountPart, AccountProjection, BrokerModel, BrokerPart,
    BrokerProjection, BusinessModel, BusinessPart, BusinessProjection, CharityPart,
    DistributionKind, GainTerm, IraKind, ItemisedModel, ItemisedPartChoice, ItemisedProjection,
    MedicalPart, MortgagePart, OtherIncomeModel, OtherIncomePart, OtherIncomeProjection,
    PriceAssumption, PropertyTaxPart, RestOfYear, RetirementModel, RetirementPart,
};

use crate::internal::civil::{self, Date};
use crate::model::{self, Context};
use crate::{reflect, validate};

/// offer is what one model's rules need: the day a new part starts, and
/// which part kinds the model holds.
struct Offer {
    from: Date,
    end: Date,
    kind: HashSet<String>,
}

/// newOffer reads a model's parts. A part starts the day after the
/// evidence, or after as_of without any, and no later than December 31.
fn new_offer<P: Message + MessageName>(
    ctx: &Context,
    parts: &[P],
    through: Option<&commonv1::Date>,
) -> Offer {
    let mut o = Offer {
        from: ctx.as_of().add_days(1),
        end: ctx.year_end(),
        kind: HashSet::new(),
    };
    if let Some(t) = through
        && let Ok(d) = civil::from_proto(Some(t))
    {
        o.from = d.add_days(1);
    }
    if o.from.after(ctx.year_end()) {
        o.from = ctx.year_end();
    }
    for p in parts {
        if let Some(d) = reflect::to_dyn(p) {
            let (_, kind, _) = validate::part_case(&d);
            o.kind.insert(kind);
        }
    }
    o
}

impl Offer {
    /// has reports whether the model holds a part of any of the kinds.
    fn has(&self, kinds: &[&str]) -> bool {
        kinds.iter().any(|k| self.kind.contains(*k))
    }

    fn start<B: ProtoBox<commonv1::Date>>(&self) -> MessageField<commonv1::Date, B> {
        self.from.field()
    }

    fn end<B: ProtoBox<commonv1::Date>>(&self) -> MessageField<commonv1::Date, B> {
        self.end.field()
    }
}

/// filled returns into with each message field that from has by the same
/// name and type.
fn filled<M, F>(into: M, from: Option<&F>) -> M
where
    M: Message + MessageName + Default,
    F: Message + MessageName,
{
    let Some(from) = from else {
        return into;
    };
    let (Some(mut dst), Some(src)) = (reflect::to_dyn(&into), reflect::to_dyn(from)) else {
        return into;
    };
    let (Some(dd), Some(sd)) = (reflect::desc(&dst), reflect::desc(&src)) else {
        return into;
    };
    for f in dd.fields() {
        let Some(sf) = reflect::field(sd, f.name()) else {
            continue;
        };
        if !reflect::is_message(f)
            || !reflect::is_message(sf)
            || reflect::message_name(f) != reflect::message_name(sf)
        {
            continue;
        }
        if let Some(v) = reflect::message(&src, sf) {
            reflect::set(&mut dst, f, Value::Message(v.clone()));
        }
    }
    reflect::from_dyn(&dst).unwrap_or(into)
}

fn zero<B: ProtoBox<commonv1::Money>>() -> MessageField<commonv1::Money, B> {
    MessageField::some(model::money(USCents(0)))
}

pub(crate) fn business_can_add(
    ctx: &Context,
    m: &BusinessModel,
    p: &BusinessProjection,
) -> Vec<BusinessPart> {
    use oneof::business_part::Part;
    use scenariosv1::business_part::{Monthly, Stops, YearTotal};
    let o = new_offer(ctx, &m.parts, p.so_far_through.as_option());
    let mut out = vec![BusinessPart {
        part: Some(Part::from(Monthly {
            from: o.start(),
            receipts: zero(),
            expenses: zero(),
            ..Default::default()
        })),
        ..Default::default()
    }];
    if !o.has(&["stopped", "stops"]) {
        out.push(BusinessPart {
            part: Some(Part::from(Stops {
                on: o.end(),
                ..Default::default()
            })),
            ..Default::default()
        });
    }
    if !o.has(&["year_total"]) {
        let total = filled(
            YearTotal::default(),
            p.year.as_option().and_then(|y| y.items.as_option()),
        );
        out.push(BusinessPart {
            part: Some(Part::from(total)),
            ..Default::default()
        });
    }
    out
}

pub(crate) fn account_can_add(
    ctx: &Context,
    m: &AccountModel,
    p: &AccountProjection,
) -> Vec<AccountPart> {
    use oneof::account_part::Part;
    use scenariosv1::account_part::{AtRate, Closes, YearEndDistribution, YearTotal};
    let o = new_offer(ctx, &m.parts, p.so_far_through.as_option());
    if o.has(&["from_forms"]) {
        return Vec::new();
    }
    let mut out = vec![
        AccountPart {
            part: Some(Part::from(AtRate {
                from: o.start(),
                ..Default::default()
            })),
            ..Default::default()
        },
        AccountPart {
            part: Some(Part::from(YearEndDistribution::default())),
            ..Default::default()
        },
    ];
    if !o.has(&["closed", "closes"]) {
        out.push(AccountPart {
            part: Some(Part::from(Closes {
                on: o.end(),
                ..Default::default()
            })),
            ..Default::default()
        });
    }
    if !o.has(&["year_total"]) {
        let total = filled(
            YearTotal::default(),
            p.year.as_option().and_then(|y| y.items.as_option()),
        );
        out.push(AccountPart {
            part: Some(Part::from(total)),
            ..Default::default()
        });
    }
    out
}

pub(crate) fn broker_can_add(
    ctx: &Context,
    m: &BrokerModel,
    p: &BrokerProjection,
) -> Vec<BrokerPart> {
    use oneof::broker_part::Part;
    use oneof::broker_part::planned_sale::Price;
    use scenariosv1::broker_part::{PlannedGain, PlannedSale};
    let o = new_offer(ctx, &m.parts, p.so_far_through.as_option());
    if o.has(&["from_forms"]) {
        return Vec::new();
    }
    vec![
        BrokerPart {
            part: Some(Part::from(PlannedSale {
                date: o.start(),
                price: Some(Price::Assumption(
                    PriceAssumption::PRICE_ASSUMPTION_TODAY.into(),
                )),
                ..Default::default()
            })),
            ..Default::default()
        },
        BrokerPart {
            part: Some(Part::from(PlannedGain {
                date: o.start(),
                term: GainTerm::GAIN_TERM_SHORT.into(),
                amount: zero(),
                ..Default::default()
            })),
            ..Default::default()
        },
    ]
}

pub(crate) fn retirement_can_add(ctx: &Context, m: &RetirementModel) -> Vec<RetirementPart> {
    use oneof::retirement_part::Part;
    use scenariosv1::retirement_part::{
        HsaContribution, IraContribution, MaxOutWorkplacePlan, WorkplacePlanTotal,
    };
    let o = new_offer(ctx, &m.parts, None);
    let mut out = Vec::new();
    if !o.has(&["max_out_workplace_plan", "workplace_plan_total"]) {
        out.push(RetirementPart {
            part: Some(Part::from(MaxOutWorkplacePlan::default())),
            ..Default::default()
        });
        out.push(RetirementPart {
            part: Some(Part::from(WorkplacePlanTotal {
                total: zero(),
                ..Default::default()
            })),
            ..Default::default()
        });
    }
    out.push(RetirementPart {
        part: Some(Part::from(IraContribution {
            date: o.start(),
            kind: IraKind::IRA_KIND_TRADITIONAL.into(),
            amount: zero(),
            ..Default::default()
        })),
        ..Default::default()
    });
    out.push(RetirementPart {
        part: Some(Part::from(HsaContribution {
            date: o.start(),
            amount: zero(),
            ..Default::default()
        })),
        ..Default::default()
    });
    out
}

pub(crate) fn other_income_can_add(
    ctx: &Context,
    m: &OtherIncomeModel,
    p: &OtherIncomeProjection,
) -> Vec<OtherIncomePart> {
    use oneof::other_income_part::Part;
    use oneof::other_income_part::one_off::Event;
    use scenariosv1::other_income_part::{OneOff, YearAmount, one_off::Distribution};
    let o = new_offer(ctx, &m.parts, p.so_far_through.as_option());
    if o.has(&["from_forms"]) {
        return Vec::new();
    }
    let mut out = vec![OtherIncomePart {
        part: Some(Part::from(OneOff {
            date: o.start(),
            event: Some(Event::from(Distribution {
                kind: DistributionKind::DISTRIBUTION_KIND_WITHDRAWAL.into(),
                gross: zero(),
                ..Default::default()
            })),
            ..Default::default()
        })),
        ..Default::default()
    }];
    if !o.has(&["year_amount"]) {
        let amount = filled(
            YearAmount::default(),
            p.year.as_option().and_then(|y| y.items.as_option()),
        );
        out.push(OtherIncomePart {
            part: Some(Part::from(amount)),
            ..Default::default()
        });
    }
    out
}

pub(crate) fn itemised_can_add(
    ctx: &Context,
    m: &ItemisedModel,
    p: &ItemisedProjection,
) -> Vec<ItemisedPartChoice> {
    use oneof::itemised_model::Kind;
    use oneof::itemised_part_choice::Part as Choice;
    let year = p.year.as_option().and_then(|y| y.items.as_option());
    let rest = p.rest.as_option().and_then(|y| y.items.as_option());
    let rest_of_year = || RestOfYear {
        amount: money0(rest),
        ..Default::default()
    };
    let mut out = Vec::new();
    match &m.kind {
        Some(Kind::Mortgage(k)) => {
            use oneof::mortgage_part::Part;
            use scenariosv1::mortgage_part::{PaidOff, Refinanced, YearTotal};
            let o = new_offer(ctx, &k.parts, p.so_far_through.as_option());
            if o.has(&["from_form_1098"]) {
                return Vec::new();
            }
            let mut add = |part: Part| {
                out.push(ItemisedPartChoice {
                    part: Some(Choice::Mortgage(Box::new(MortgagePart {
                        part: Some(part),
                        ..Default::default()
                    }))),
                    ..Default::default()
                });
            };
            add(Part::from(Refinanced {
                from: o.start(),
                ..Default::default()
            }));
            if !o.has(&["repaid", "paid_off"]) {
                add(Part::from(PaidOff {
                    on: o.end(),
                    ..Default::default()
                }));
            }
            if !o.has(&["year_total", "rest_of_year"]) {
                add(Part::from(YearTotal {
                    interest: money0(year),
                    ..Default::default()
                }));
                add(Part::RestOfYear(Box::new(rest_of_year())));
            }
        }
        Some(Kind::PropertyTax(k)) => {
            use oneof::property_tax_part::Part;
            use scenariosv1::property_tax_part::{PlannedInstallment, YearTotal};
            let o = new_offer(ctx, &k.parts, p.so_far_through.as_option());
            let mut add = |part: Part| {
                out.push(ItemisedPartChoice {
                    part: Some(Choice::PropertyTax(Box::new(PropertyTaxPart {
                        part: Some(part),
                        ..Default::default()
                    }))),
                    ..Default::default()
                });
            };
            add(Part::from(PlannedInstallment {
                due: o.start(),
                amount: zero(),
                ..Default::default()
            }));
            if !o.has(&["year_total", "rest_of_year"]) {
                add(Part::from(YearTotal {
                    total: money0(year),
                    ..Default::default()
                }));
                add(Part::RestOfYear(Box::new(rest_of_year())));
            }
        }
        Some(Kind::Charity(k)) => {
            use oneof::charity_part::Part;
            use scenariosv1::charity_part::{LikeLastYear, PlannedGift};
            let o = new_offer(ctx, &k.parts, p.so_far_through.as_option());
            let mut add = |part: Part| {
                out.push(ItemisedPartChoice {
                    part: Some(Choice::Charity(Box::new(CharityPart {
                        part: Some(part),
                        ..Default::default()
                    }))),
                    ..Default::default()
                });
            };
            add(Part::from(PlannedGift {
                date: o.start(),
                amount: zero(),
                ..Default::default()
            }));
            if !o.has(&["like_last_year", "rest_of_year"]) {
                add(Part::from(LikeLastYear {
                    from: o.start(),
                    ..Default::default()
                }));
                add(Part::RestOfYear(Box::new(rest_of_year())));
            }
        }
        Some(Kind::Medical(k)) => {
            use oneof::medical_part::Part;
            use scenariosv1::medical_part::{PlannedExpense, YearTotal};
            let o = new_offer(ctx, &k.parts, p.so_far_through.as_option());
            let mut add = |part: Part| {
                out.push(ItemisedPartChoice {
                    part: Some(Choice::Medical(Box::new(MedicalPart {
                        part: Some(part),
                        ..Default::default()
                    }))),
                    ..Default::default()
                });
            };
            add(Part::from(PlannedExpense {
                date: o.start(),
                amount: zero(),
                ..Default::default()
            }));
            if !o.has(&["year_total", "rest_of_year"]) {
                add(Part::from(YearTotal {
                    total: money0(year),
                    ..Default::default()
                }));
                add(Part::RestOfYear(Box::new(rest_of_year())));
            }
        }
        None => {}
    }
    out
}

/// money0 is a copy of an amount, or $0 when it is unset.
fn money0<B: ProtoBox<commonv1::Money>>(
    m: Option<&commonv1::Money>,
) -> MessageField<commonv1::Money, B> {
    match m {
        Some(m) => MessageField::some(m.clone()),
        None => zero(),
    }
}
