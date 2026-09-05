export function openDialog({ title, render }) {
  if (typeof render !== "function") {
    throw new Error("dialog: render(container, close) is required");
  }
  const overlay = document.createElement("div");
  overlay.className = "modal-overlay";
  const modal = document.createElement("div");
  modal.className = "modal";
  if (title) {
    const h = document.createElement("div");
    h.style.cssText = "font-size:15px;font-weight:700;color:var(--color-text);margin-bottom:6px;";
    h.textContent = title;
    modal.appendChild(h);
  }
  const body = document.createElement("div");
  modal.appendChild(body);
  overlay.appendChild(modal);
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    overlay.remove();
  };
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  document.body.appendChild(overlay);
  render(body, close);
  return { close };
}
