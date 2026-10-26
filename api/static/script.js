(function() {
    const saved = localStorage.getItem('theme');
    const systemDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const theme = saved || (systemDark ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', theme)
})();

document.getElementById('theme').onclick = () => {
    const r = document.documentElement;
    const newTheme = r.dataset.theme == 'dark' ? 'light' : 'dark';
    r.dataset.theme = newTheme;
    localStorage.setItem('theme', newTheme);
}