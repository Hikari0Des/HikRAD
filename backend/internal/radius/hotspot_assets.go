package radius

// Embedded MikroTik Hotspot login-page assets (FR-18). login.html uses RouterOS
// $(...) template variables the router substitutes at serve time; branding
// placeholders __HikRAD_*__ are substituted by hotspot.go from settings. The
// page supports http-chap (hashing the password with md5.js when $(chap-id) is
// present) and falls back to http-pap (plaintext) otherwise, and offers a
// voucher-code login where the code is submitted as both username and password
// so B's authorize path can detect + redeem it (FR-18.1).

// hotspotLoginHTML is the themable login.html. Placeholders:
//   __HikRAD_NAME__   ISP display name
//   __HikRAD_PRIMARY__ primary brand colour (CSS)
//   __HikRAD_LOGO__   logo markup (inline <img> data URI, or a text fallback)
const hotspotLoginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>__HikRAD_NAME__ &mdash; Wi-Fi Login</title>
<link rel="stylesheet" href="style.css">
<script src="md5.js" type="text/javascript"></script>
<script type="text/javascript">
var chapId = '$(chap-id)';
var chapChallenge = '$(chap-challenge)';
function enc(pw) { return (chapId.length && typeof hexMD5 === 'function') ? hexMD5(chapId + pw + chapChallenge) : pw; }
function post(u, pw) {
  var f = document.forms['sendin'];
  f.username.value = u;
  f.password.value = enc(pw);
  f.submit();
  return false;
}
function doLogin() { return post(document.forms['login'].username.value, document.forms['login'].password.value); }
function doVoucher() {
  var v = document.forms['voucher'].code.value.replace(/\s+/g, '');
  if (!v) { return false; }
  return post(v, v);
}
function showTab(which) {
  document.getElementById('tab-account').style.display = which === 'account' ? 'block' : 'none';
  document.getElementById('tab-voucher').style.display = which === 'voucher' ? 'block' : 'none';
  document.getElementById('btn-account').className = which === 'account' ? 'tab active' : 'tab';
  document.getElementById('btn-voucher').className = which === 'voucher' ? 'tab active' : 'tab';
  return false;
}
</script>
</head>
<body>
<div class="card">
  <div class="brand">__HikRAD_LOGO__<h1>__HikRAD_NAME__</h1></div>
  $(if error)<div class="error">$(error)</div>$(endif)

  <div class="tabbar">
    <a href="#" id="btn-account" class="tab active" onclick="return showTab('account')">Account</a>
    <a href="#" id="btn-voucher" class="tab" onclick="return showTab('voucher')">Voucher</a>
  </div>

  <!-- Hidden form actually submitted to the NAS (carries CHAP-hashed password). -->
  <form name="sendin" action="$(link-login-only)" method="post" style="display:none">
    <input type="hidden" name="dst" value="$(link-orig)">
    <input type="hidden" name="popup" value="true">
    <input type="hidden" name="username">
    <input type="hidden" name="password">
  </form>

  <div id="tab-account">
    <form name="login" onsubmit="return doLogin()">
      <label>Username<input name="username" type="text" autocomplete="username" autofocus></label>
      <label>Password<input name="password" type="password" autocomplete="current-password"></label>
      <button type="submit">Log in</button>
    </form>
  </div>

  <div id="tab-voucher" style="display:none">
    <form name="voucher" onsubmit="return doVoucher()">
      <label>Voucher code<input name="code" type="text" inputmode="latin" autocapitalize="characters" placeholder="XXXX-XXXX"></label>
      <button type="submit">Redeem &amp; connect</button>
    </form>
  </div>

  <p class="foot">Powered by __HikRAD_NAME__</p>
</div>
</body>
</html>
`

// hotspotStyleCSS is the themable stylesheet. __HikRAD_PRIMARY__ is the brand
// colour. Deliberately dependency-free (served locally by the router).
const hotspotStyleCSS = `:root { --primary: __HikRAD_PRIMARY__; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  font-family: -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; background: #f2f4f7; color: #111827; }
.card { width: 100%; max-width: 360px; margin: 16px; background: #fff; border-radius: 14px;
  box-shadow: 0 8px 30px rgba(0,0,0,.08); padding: 28px 24px; }
.brand { text-align: center; margin-bottom: 8px; }
.brand img { max-height: 64px; max-width: 100%; }
.brand h1 { font-size: 20px; margin: 10px 0 0; }
.tabbar { display: flex; gap: 6px; margin: 18px 0; border-bottom: 1px solid #e5e7eb; }
.tab { flex: 1; text-align: center; padding: 10px; text-decoration: none; color: #6b7280;
  border-bottom: 2px solid transparent; font-weight: 600; }
.tab.active { color: var(--primary); border-bottom-color: var(--primary); }
label { display: block; font-size: 13px; color: #374151; margin-bottom: 12px; }
input { width: 100%; margin-top: 4px; padding: 11px 12px; border: 1px solid #d1d5db; border-radius: 9px; font-size: 15px; }
input:focus { outline: none; border-color: var(--primary); }
button { width: 100%; margin-top: 6px; padding: 12px; border: 0; border-radius: 9px; background: var(--primary);
  color: #fff; font-size: 15px; font-weight: 600; cursor: pointer; }
button:hover { filter: brightness(.95); }
.error { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; border-radius: 9px; padding: 10px; font-size: 14px; margin-bottom: 8px; }
.foot { text-align: center; color: #9ca3af; font-size: 12px; margin: 18px 0 0; }
`

// hotspotMD5JS is Paul Johnston's public-domain JS MD5 (RSA Data Security), the
// same routine RouterOS's stock login page uses. Exposes hexMD5 for the CHAP
// path. Distributed under the BSD licence.
const hotspotMD5JS = `/*
 * A JavaScript implementation of the RSA Data Security, Inc. MD5 Message
 * Digest Algorithm, as defined in RFC 1321.
 * Copyright (C) Paul Johnston 1999 - 2002. BSD License.
 */
var hexcase = 0;
function hexMD5(s){ return binl2hex(core_md5(str2binl(s), s.length * 8)); }
function safe_add(x, y){ var lsw=(x&0xFFFF)+(y&0xFFFF); var msw=(x>>16)+(y>>16)+(lsw>>16); return (msw<<16)|(lsw&0xFFFF); }
function bit_rol(num, cnt){ return (num<<cnt)|(num>>>(32-cnt)); }
function md5_cmn(q,a,b,x,s,t){ return safe_add(bit_rol(safe_add(safe_add(a,q),safe_add(x,t)),s),b); }
function md5_ff(a,b,c,d,x,s,t){ return md5_cmn((b&c)|((~b)&d),a,b,x,s,t); }
function md5_gg(a,b,c,d,x,s,t){ return md5_cmn((b&d)|(c&(~d)),a,b,x,s,t); }
function md5_hh(a,b,c,d,x,s,t){ return md5_cmn(b^c^d,a,b,x,s,t); }
function md5_ii(a,b,c,d,x,s,t){ return md5_cmn(c^(b|(~d)),a,b,x,s,t); }
function core_md5(x, len){
  x[len>>5]|=0x80<<((len)%32); x[(((len+64)>>>9)<<4)+14]=len;
  var a=1732584193, b=-271733879, c=-1732584194, d=271733878;
  for(var i=0;i<x.length;i+=16){
    var olda=a, oldb=b, oldc=c, oldd=d;
    a=md5_ff(a,b,c,d,x[i+0],7,-680876936); d=md5_ff(d,a,b,c,x[i+1],12,-389564586); c=md5_ff(c,d,a,b,x[i+2],17,606105819); b=md5_ff(b,c,d,a,x[i+3],22,-1044525330);
    a=md5_ff(a,b,c,d,x[i+4],7,-176418897); d=md5_ff(d,a,b,c,x[i+5],12,1200080426); c=md5_ff(c,d,a,b,x[i+6],17,-1473231341); b=md5_ff(b,c,d,a,x[i+7],22,-45705983);
    a=md5_ff(a,b,c,d,x[i+8],7,1770035416); d=md5_ff(d,a,b,c,x[i+9],12,-1958414417); c=md5_ff(c,d,a,b,x[i+10],17,-42063); b=md5_ff(b,c,d,a,x[i+11],22,-1990404162);
    a=md5_ff(a,b,c,d,x[i+12],7,1804603682); d=md5_ff(d,a,b,c,x[i+13],12,-40341101); c=md5_ff(c,d,a,b,x[i+14],17,-1502002290); b=md5_ff(b,c,d,a,x[i+15],22,1236535329);
    a=md5_gg(a,b,c,d,x[i+1],5,-165796510); d=md5_gg(d,a,b,c,x[i+6],9,-1069501632); c=md5_gg(c,d,a,b,x[i+11],14,643717713); b=md5_gg(b,c,d,a,x[i+0],20,-373897302);
    a=md5_gg(a,b,c,d,x[i+5],5,-701558691); d=md5_gg(d,a,b,c,x[i+10],9,38016083); c=md5_gg(c,d,a,b,x[i+15],14,-660478335); b=md5_gg(b,c,d,a,x[i+4],20,-405537848);
    a=md5_gg(a,b,c,d,x[i+9],5,568446438); d=md5_gg(d,a,b,c,x[i+14],9,-1019803690); c=md5_gg(c,d,a,b,x[i+3],14,-187363961); b=md5_gg(b,c,d,a,x[i+8],20,1163531501);
    a=md5_gg(a,b,c,d,x[i+13],5,-1444681467); d=md5_gg(d,a,b,c,x[i+2],9,-51403784); c=md5_gg(c,d,a,b,x[i+7],14,1735328473); b=md5_gg(b,c,d,a,x[i+12],20,-1926607734);
    a=md5_hh(a,b,c,d,x[i+5],4,-378558); d=md5_hh(d,a,b,c,x[i+8],11,-2022574463); c=md5_hh(c,d,a,b,x[i+11],16,1839030562); b=md5_hh(b,c,d,a,x[i+14],23,-35309556);
    a=md5_hh(a,b,c,d,x[i+1],4,-1530992060); d=md5_hh(d,a,b,c,x[i+4],11,1272893353); c=md5_hh(c,d,a,b,x[i+7],16,-155497632); b=md5_hh(b,c,d,a,x[i+10],23,-1094730640);
    a=md5_hh(a,b,c,d,x[i+13],4,681279174); d=md5_hh(d,a,b,c,x[i+0],11,-358537222); c=md5_hh(c,d,a,b,x[i+3],16,-722521979); b=md5_hh(b,c,d,a,x[i+6],23,76029189);
    a=md5_hh(a,b,c,d,x[i+9],4,-640364487); d=md5_hh(d,a,b,c,x[i+12],11,-421815835); c=md5_hh(c,d,a,b,x[i+15],16,530742520); b=md5_hh(b,c,d,a,x[i+2],23,-995338651);
    a=md5_ii(a,b,c,d,x[i+0],6,-198630844); d=md5_ii(d,a,b,c,x[i+7],10,1126891415); c=md5_ii(c,d,a,b,x[i+14],15,-1416354905); b=md5_ii(b,c,d,a,x[i+5],21,-57434055);
    a=md5_ii(a,b,c,d,x[i+12],6,1700485571); d=md5_ii(d,a,b,c,x[i+3],10,-1894986606); c=md5_ii(c,d,a,b,x[i+10],15,-1051523); b=md5_ii(b,c,d,a,x[i+1],21,-2054922799);
    a=md5_ii(a,b,c,d,x[i+8],6,1873313359); d=md5_ii(d,a,b,c,x[i+15],10,-30611744); c=md5_ii(c,d,a,b,x[i+6],15,-1560198380); b=md5_ii(b,c,d,a,x[i+13],21,1309151649);
    a=md5_ii(a,b,c,d,x[i+4],6,-145523070); d=md5_ii(d,a,b,c,x[i+11],10,-1120210379); c=md5_ii(c,d,a,b,x[i+2],15,718787259); b=md5_ii(b,c,d,a,x[i+9],21,-343485551);
    a=safe_add(a,olda); b=safe_add(b,oldb); c=safe_add(c,oldc); d=safe_add(d,oldd);
  }
  return [a,b,c,d];
}
function str2binl(str){ var bin=[]; var mask=(1<<8)-1; for(var i=0;i<str.length*8;i+=8) bin[i>>5]|=(str.charCodeAt(i/8)&mask)<<(i%32); return bin; }
function binl2hex(binarray){ var hex_tab=hexcase?"0123456789ABCDEF":"0123456789abcdef"; var str=""; for(var i=0;i<binarray.length*4;i++){ str+=hex_tab.charAt((binarray[i>>2]>>((i%4)*8+4))&0xF)+hex_tab.charAt((binarray[i>>2]>>((i%4)*8))&0xF); } return str; }
`
