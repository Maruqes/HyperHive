import { DurableObject, WorkflowEntrypoint } from "cloudflare:workers";
export class ProbeCell extends DurableObject {
  async fetch(request) {
    console.log("cell probe");
    const n = (await this.ctx.storage.get("n") || 0) + 1;
    await this.ctx.storage.put("n", n);
    if (new URL(request.url).pathname === "/facet") {
      const worker = this.env.LOADER.get("facet", () => ({compatibilityDate:"2025-01-01",mainModule:"app.js",modules:{"app.js":"import { DurableObject } from 'cloudflare:workers'; export class Child extends DurableObject { fetch() { return new Response('facet'); } }"}}));
      return this.ctx.facets.get("child", () => ({class:worker.getDurableObjectClass("Child")})).fetch(request);
    }
    return Response.json({n});
  }
}
export class ProbeWorkflow extends WorkflowEntrypoint {
  async run(event, step) {
    await step.do("begin", () => { console.log("workflow begin"); return true; });
    if (event.payload.fail) throw new Error("probe workflow failure");
    await step.sleep("waiting", "5 minutes");
    return "done";
  }
}
export default {
  async fetch(request, env) {
    const path = new URL(request.url).pathname;
    console.log("probe request", path);
    if (path === "/failure") { console.error("probe console error"); throw new Error("probe request failure"); }
    if (path === "/cell" || path === "/facet") return env.CELLS.getByName("probe").fetch(request);
    if (path === "/queue") { await env.QUEUE.send({probe:true}); return new Response("queued"); }
    if (path === "/workflow" || path === "/workflow-failure") { const job = await env.JOBS.create({params:{fail:path.endsWith("failure")}}); return Response.json({id:job.id,status:await job.status()}); }
    if (path === "/bindings") {
      await env.DB.prepare("CREATE TABLE IF NOT EXISTS probe (n INTEGER)").run();
      await env.KV.put("probe", "ok"); await env.KV.get("probe");
      await env.FILES.put("probe", "ok"); await env.FILES.get("probe");
      return new Response("bindings ok");
    }
    if (path === "/dynamic") {
      const worker = env.LOADER.get("probe", () => ({compatibilityDate:"2025-01-01",mainModule:"app.js",modules:{"app.js":"export default { fetch() { console.log('dynamic probe'); return new Response('dynamic'); } }"}}));
      return worker.getEntrypoint().fetch(request);
    }
    return new Response("idle worker");
  },
  async queue(batch) { console.log("queue delivered", batch.messages.length); batch.ackAll(); },
  async scheduled(event) { console.log("cron probe", event.scheduledTime); }
};
