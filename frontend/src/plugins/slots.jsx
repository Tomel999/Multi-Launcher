import { useEffect, useRef, useState } from "preact/hooks";
const registry = /* @__PURE__ */ new Map();
const listeners = /* @__PURE__ */ new Set();
function bump() {
  for (const l of listeners) l();
}
export function registerSlot(name, id, render) {
  if (!registry.has(name)) registry.set(name, []);
  registry.get(name).push({ id, render });
  bump();
}
export function unregisterSlot(name, id) {
  const arr = registry.get(name);
  if (!arr) return;
  registry.set(name, arr.filter((s) => s.id !== id));
  bump();
}
export function unregisterAllSlots(id) {
  for (const [name, arr] of registry) {
    const filtered = arr.filter((s) => s.id !== id);
    if (filtered.length !== arr.length) registry.set(name, filtered);
  }
  bump();
}
export function Slot({ name, fallback = null, ctx = null }) {
  const [, setV] = useState(0);
  useEffect(() => {
    const l = () => setV((v) => v + 1);
    listeners.add(l);
    return () => listeners.delete(l);
  }, []);
  const items = registry.get(name) || [];
  if (!items.length) return fallback;
  return <>{items.map((s) => <SlotItem key={s.id} render={s.render} ctx={ctx} />)}</>;
}
function SlotItem({ render, ctx }) {
  const ref = useRef(null);
  useEffect(() => {
    const el = render(ctx);
    if (el && ref.current) ref.current.appendChild(el);
    return () => {
      if (el) el.remove();
    };
  }, []);
  return <div ref={ref} />;
}
