// Generator of v3_parity.json — run from a budget-ui checkout:
//   npx esbuild services/testdata/v3_parity.gen.ts --bundle --platform=node --format=esm \
//     --alias:@=<budget-ui>/src --outfile=/tmp/gen.mjs && TZ=Europe/Paris node /tmp/gen.mjs v3_parity.json 15
// Random v3 budgets go through the real UI codec (decode → auto-close → encode),
// and the UI engine month totals are recorded next to the stored blob.
import { decodeBudget, autoCloseMonths, encodeBudget } from '@/lib/budget/codec';
import { BudgetEngine } from '@/lib/budget/engine';
import { writeFileSync } from 'node:fs';

let s = 1;
const rnd = () => ((s = (s * 1103515245 + 12345) % 2147483648) / 2147483648);
const int = (n: number) => Math.floor(rnd() * n);
const money = (max: number) => Math.round(rnd() * max * 100) / 100;
const ym = (y: number, m: number) => `${y}-${String(m).padStart(2, '0')}`;
const anyYM = () => ym(2025 + int(3), 1 + int(12));
const steps = () => {
  if (int(2)) return undefined;
  const out = [] as { from: string; amount: number }[];
  for (let i = 0; i < 1 + int(3); i++) out.push({ from: anyYM(), amount: int(5) === 0 ? 0 : money(1500) });
  return out;
};
const overrides = () => {
  if (int(2)) return undefined;
  const o: Record<string, number> = {};
  for (let i = 0; i < 1 + int(3); i++) o[anyYM()] = int(3) === 0 ? 0 : money(900);
  return o;
};
const date = () => (int(3) ? undefined : `${anyYM()}-${String(1 + int(28)).padStart(2, '0')}`);

function raw(): any {
  const people = Array.from({ length: 1 + int(3) }, (_, i) => ({
    id: `p${i}`, name: `P${i}`, salary: money(5000), startDate: date(), endDate: date(),
    salaryHistory: steps(), salaryOverrides: overrides(),
    contributions: int(2) ? undefined : [{ from: anyYM(), mode: (['all', 'fixed', 'percent'] as const)[int(3)], value: money(100) * 20 }, ...(int(2) ? [{ from: anyYM(), mode: 'percent' as const, value: int(100) }] : [])],
    contributionOverrides: overrides(),
  }));
  const freqs = [undefined, 'monthly', 'custom', 'yearly', 'once'] as const;
  const charges = Array.from({ length: int(7) }, (_, i) => {
    const f = freqs[int(5)];
    return {
      id: `c${i}`, label: `C${i}`, amount: money(1500), startDate: f === 'once' ? `${anyYM()}-01` : date(), endDate: date(),
      frequency: f, months: f === 'custom' ? Array.from(new Set(Array.from({ length: 1 + int(8) }, () => 1 + int(12)))) : undefined,
      smooth: f === 'yearly' ? !!int(2) : undefined, amountHistory: steps(), overrides: overrides(),
    };
  });
  const projects = Array.from({ length: int(4) }, (_, i) => ({
    id: `s${i}`, label: `S${i}`, targetAmount: money(4000),
    ...(int(2) ? { monthlyAmount: money(300), startDate: date(), endDate: date(), amountHistory: steps(), overrides: overrides() } : {}),
  }));
  const yearlyData: any = {};
  const oneTimeIncomes: any = {};
  for (const y of [2025, 2026]) {
    yearlyData[y] = {
      months: Array.from({ length: 12 }, () => Object.fromEntries(projects.filter(() => int(2)).map((p) => [p.id, money(300)]))),
      expenses: Array.from({ length: 12 }, () => (int(4) ? {} : { epargne: money(200) })),
      monthComments: Array(12).fill(''), expenseComments: Array.from({ length: 12 }, () => ({})),
      lockedMonths: y === 2026 && int(2) ? { Mars: false, Novembre: true } : {},
    };
    oneTimeIncomes[y] = Array.from({ length: 12 }, () => (int(4) ? { amount: 0 } : { amount: money(700), description: 'Prime' }));
  }
  return { budgetTitle: 'T', currentYear: 2026, people, charges, projects, yearlyData, oneTimeIncomes, lockedMonths: {} };
}

const today = '2026-10';
const out = [];
for (let n = 0; n < Number(process.argv[3] ?? 15); n++) {
  const r = raw();
  let model = autoCloseMonths(decodeBudget(r, today), today, '2026-10-02T00:00:00Z').model;
  // Change the rules after closing so frozen months differ from the rules.
  model = { ...model, people: model.people.map((p) => ({ ...p, salary: p.salary + 111, salaryHistory: undefined })), charges: model.charges.map((c) => ({ ...c, amount: c.amount + 7, amountHistory: undefined })) };
  const blob = JSON.parse(JSON.stringify(encodeBudget(model, today, '2026-10-02T00:00:00Z')));
  const engine = new BudgetEngine(decodeBudget(blob, today), today);
  const months: Record<string, any> = {};
  for (const y of [2025, 2026, 2027]) for (let m = 1; m <= 12; m++) {
    const t = engine.month(ym(y, m)).totals;
    months[ym(y, m)] = { contributions: t.contributions, oneOff: t.oneOff, charges: t.charges, savings: t.savings };
  }
  out.push({ blob, months });
}
writeFileSync(process.argv[2], JSON.stringify(out));
console.log('ok', out.length);
