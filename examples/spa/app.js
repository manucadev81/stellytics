// SPA sem framework: troca o conteúdo e a URL com history.pushState, sem
// recarregar a página. O script de rastreamento conta cada mudança de rota.
const routes = {
  "/": { title: "SPA: início", body: "Página inicial da SPA." },
  "/precos": { title: "SPA: preços", body: "Tabela de preços." },
  "/contato": { title: "SPA: contato", body: "Fale com a gente." },
};

function render() {
  const route = routes[location.pathname] ?? { title: "SPA: não encontrada", body: "Página não encontrada." };
  document.title = route.title;
  document.getElementById("view").textContent = route.body;
}

document.addEventListener("click", (event) => {
  const link = event.target.closest("a[data-route]");
  if (!link) return;
  event.preventDefault();
  if (link.pathname === location.pathname) return;
  history.pushState(null, "", link.pathname);
  render();
});

addEventListener("popstate", render);
render();
