// vnx.tools: www.vnx.tools redirects to vnx.tools; every other request is a
// static asset from dist/ (with the headers in dist/_headers).
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.hostname === "www.vnx.tools") {
      url.hostname = "vnx.tools";
      return Response.redirect(url.toString(), 301);
    }
    return env.ASSETS.fetch(request);
  },
};
