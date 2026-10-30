(() => {
  let preference = 'system'
  try {
    const saved = localStorage.getItem('sefir-theme')
    if (saved === 'light' || saved === 'dark') preference = saved
  } catch {}
  const dark = preference === 'dark' || (preference === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = preference
  document.documentElement.classList.toggle('dark', dark)
})()
