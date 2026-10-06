const B = "http://127.0.0.1:4000"
const uniq = Date.now().toString(36)
const r0 = await fetch(B + "/login")
const h0 = await r0.text()
const ck0 = (r0.headers.get("set-cookie") || "").split(";")[0]
const r1 = await fetch(B + "/login", { method: "POST", headers: { "content-type": "application/json", accept: "application/json", cookie: ck0, "x-csrf-token": h0.match(/ol-csrfToken" content="([^"]+)"/)[1] }, body: JSON.stringify({ email: "e2e-admin@e2e.test", password: "Ol-Fixture-9x7K" }) })
const ck = (r1.headers.get("set-cookie") || "").split(";")[0] || ck0
const rl = await fetch(B + "/login", { headers: { cookie: ck } })
const csrf = (await rl.text()).match(/ol-csrfToken" content="([^"]+)"/)[1]
async function post(name, color) {
  const body = {}
  if (name !== undefined) body.name = name
  if (color !== undefined) body.color = color
  const r = await fetch(B + "/tag", { method: "POST", headers: { "content-type": "application/json", accept: "application/json", "x-csrf-token": csrf, cookie: ck }, body: JSON.stringify(body) })
  return [r.status, (await r.text())]
}
const n1 = "ordtest-" + uniq, n2 = "ordtest2-" + uniq
console.log("NEW nocolor :", await post(n1))
console.log("NEW color   :", await post(n2, "#1234ab"))
console.log("DUP nocolor :", await post(n1))
console.log("DUP color   :", await post(n2, "#ffff00"))
