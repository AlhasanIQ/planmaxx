export function htmlPreviewDocument(source: string, theme: "light" | "dark"): string {
  const background = theme === "dark" ? "#111831" : "#ffffff";
  const foreground = theme === "dark" ? "#e6ebf5" : "#0f172a";
  const muted = theme === "dark" ? "#9aa6bc" : "#5b6573";
  const border = theme === "dark" ? "#2e3b66" : "#d8dee8";
  const csp = [
    "default-src 'none'",
    "script-src 'none'",
    "style-src 'unsafe-inline'",
    "img-src data:",
    "font-src data:",
    "media-src data:",
    "connect-src 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ");

  return `<!doctype html>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  :root { color-scheme: ${theme}; }
  html { background: ${background}; color: ${foreground}; }
  body { box-sizing: border-box; margin: 0; padding: 24px; font: 14px/1.6 ui-sans-serif, system-ui, sans-serif; }
  *, *::before, *::after { box-sizing: inherit; }
  img, video, canvas, svg, table, pre { max-width: 100%; }
  table { border-collapse: collapse; }
  th, td { border: 1px solid ${border}; padding: 6px 8px; }
  pre { overflow: auto; }
  code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  blockquote { margin-left: 0; padding-left: 1em; border-left: 3px solid ${border}; color: ${muted}; }
</style>
${sanitizeHTMLPreviewSource(source)}
<style>
  script, iframe, frame, object, embed, form, input, button, select, textarea { display: none !important; }
  a { pointer-events: none !important; }
</style>`;
}

const blockedTags = "script,iframe,frame,object,embed,applet,form,input,button,select,textarea,template,meta,base,link,animate,set,animatemotion,animatetransform";
const resourceAttributes = new Set([
  "action",
  "background",
  "cite",
  "data",
  "dynsrc",
  "formaction",
  "href",
  "longdesc",
  "lowsrc",
  "ping",
  "poster",
  "src",
  "srcdoc",
  "srcset",
  "xlink:href",
]);

export function sanitizeHTMLPreviewSource(source: string): string {
  // DOMParser is provided by the review browser. The fallback keeps this
  // string builder usable in non-DOM unit tests; the iframe sandbox and CSP
  // remain the runtime security boundary in either case.
  if (typeof DOMParser === "undefined") return source;

  const document = new DOMParser().parseFromString(source, "text/html");
  document.querySelectorAll(blockedTags).forEach((element) => element.remove());
  document.querySelectorAll("*").forEach((element) => {
    for (const attribute of [...element.attributes]) {
      const name = attribute.name.toLowerCase();
      const safeRasterImage =
        name === "src" &&
        element.tagName.toLowerCase() === "img" &&
        /^data:image\/(?:avif|gif|jpeg|png|webp);base64,/i.test(attribute.value);
      if (safeRasterImage) continue;
      if (name.startsWith("on") || resourceAttributes.has(name)) {
        element.removeAttribute(attribute.name);
      }
    }
  });
  return document.documentElement.outerHTML;
}
