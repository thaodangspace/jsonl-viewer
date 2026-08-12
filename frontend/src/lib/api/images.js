/** Build a URL to view an image referenced by a session message. */
export function imageViewUrl(absPath) {
  const encoded = btoa(absPath)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
  return `/api/images/view?p=${encoded}`;
}

function btoa(str) {
  const bytes = new TextEncoder().encode(str);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return window.btoa(binary);
}
