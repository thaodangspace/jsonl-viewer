/** Translate text using the server's local LLM. */
export async function translateText(text, targetLang = 'vi') {
  const res = await fetch('/api/translate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, target_lang: targetLang }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
