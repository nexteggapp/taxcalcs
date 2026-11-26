"""Runs households through PolicyEngine-US.

Reads one household per line of JSON from the file named by the first
argument and writes one result per line to the file named by the second.

A household is {"id", "year", "state", "joint", "people": [{"age",
"wages", "interest", "pension", "social_security"}]}; amounts are dollars.
A result is {"id", "figures": {variable: dollars}, "error"}.
"""

import json
import sys

from policyengine_us import Simulation

# The variables the harness compares, by PolicyEngine-US name.
VARIABLES = [
    "adjusted_gross_income",
    "taxable_income",
    "income_tax_before_credits",
    "income_tax",
    "state_agi",
    "state_taxable_income",
    "state_income_tax_before_refundable_credits",
    "state_income_tax",
]


def situation(h):
    year = str(h["year"])
    people, members = {}, []
    for i, p in enumerate(h["people"]):
        name = "person%d" % i
        members.append(name)
        people[name] = {
            "age": {year: p["age"]},
            "employment_income": {year: p.get("wages", 0)},
            "taxable_interest_income": {year: p.get("interest", 0)},
            # A 1099-R pension from a private employer plan. PolicyEngine-US
            # adds taxable_pension_income from its public and private parts,
            # and state exclusions read the parts.
            "taxable_private_pension_income": {year: p.get("pension", 0)},
            "social_security_retirement": {year: p.get("social_security", 0)},
            # PolicyEngine-US imputes the Alaska Permanent Fund Dividend to
            # every Alaska resident; the households have no such income.
            "ak_permanent_fund_dividend": {year: 0},
            # PolicyEngine-US imputes a Medicare Part B premium to everyone
            # over 65; the households paid no premiums.
            "medicare_part_b_premium": {year: 0},
        }
    unit = {"members": members}
    return {
        "people": people,
        "families": {"family": dict(unit)},
        "marital_units": {"marital_unit": dict(unit)},
        # PolicyEngine-US imputes a premium tax credit to a household without
        # employer coverage; the households have no Marketplace coverage.
        # The credit reduces Oregon's federal tax subtraction.
        "tax_units": {"tax_unit": dict(unit, aca_ptc={year: 0})},
        "spm_units": {"spm_unit": dict(unit)},
        "households": {"household": dict(unit, state_name={year: h["state"]})},
    }


def run(h):
    try:
        sim = Simulation(situation=situation(h))
        figures = {v: round(float(sim.calculate(v, h["year"])[0]), 2) for v in VARIABLES}
        return {"id": h["id"], "figures": figures}
    except Exception as e:  # A household PolicyEngine cannot compute is reported, not fatal.
        return {"id": h["id"], "error": "%s: %s" % (type(e).__name__, e)}


def main():
    with open(sys.argv[1]) as src, open(sys.argv[2], "w") as dst:
        for line in src:
            if line.strip():
                dst.write(json.dumps(run(json.loads(line))) + "\n")


if __name__ == "__main__":
    main()
