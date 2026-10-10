// keys.js 「密钥分发」页的前端逻辑（同源脚本，供 CSP script-src 'self' 加载）。
// 与 app.js 分开：CSP 禁内联脚本，页面里不能直接写 <script> 块。
(function(){
  var KEY = localStorage.getItem('wb2api_key') || '';
  function hdr(){ var h={'Content-Type':'application/json'}; if(KEY) h['Authorization']='Bearer '+KEY; return h; }
  function el(id){ return document.getElementById(id); }
  function say(id, cls, text){ var e=el(id); e.className='msg show '+cls; e.textContent=text; }
  function hide(id){ el(id).className='msg'; }
  function esc(s){ return String(s==null?'':s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];}); }

  var revealed = false;

  function load(){
    hide('listMsg');
    fetch('/panel/api/keys' + (revealed ? '?reveal=1' : ''), {headers: hdr()})
      .then(function(r){
        if(r.status === 401){ throw new Error('未授权：请先回控制台用 api_key 登录一次（本页与它共用同一把密钥）'); }
        return r.json();
      })
      .then(function(j){
        if(!j.ok){ throw new Error(j.error || '加载失败'); }
        el('baseUrl').textContent = location.origin + '/v1';
        el('count').textContent = '（' + j.keys.length + ' / ' + j.max_count + '）';
        var tb = el('rows');
        if(!j.keys.length){ tb.innerHTML = '<tr><td colspan="6" style="color:var(--dim)">还没有签发过密钥</td></tr>'; return; }
        tb.innerHTML = j.keys.map(function(k){
          return '<tr>'
            + '<td>' + esc(k.name) + '</td>'
            + '<td><code>' + esc(k.secret) + '</code></td>'
            + '<td><span class="tag ' + (k.enabled?'on':'off') + '">' + (k.enabled?'启用':'停用') + '</span></td>'
            + '<td style="color:var(--dim)">' + esc(k.note||'—') + '</td>'
            + '<td style="color:var(--dim);font-size:12px">' + esc((k.created_at||'').replace('T',' ').slice(0,19)||'—') + '</td>'
            + '<td><button class="ghost xs" data-act="toggle" data-name="' + esc(k.name) + '">' + (k.enabled?'停用':'启用') + '</button> '
            + '<button class="danger xs" data-act="remove" data-name="' + esc(k.name) + '">删除</button></td>'
          + '</tr>';
        }).join('');
      })
      .catch(function(e){ say('listMsg','err', e.message); });
  }

  el('rows').addEventListener('click', function(ev){
    var b = ev.target.closest ? ev.target.closest('button[data-act]') : null;
    if(!b) return;
    var act = b.getAttribute('data-act'), name = b.getAttribute('data-name');
    if(act === 'remove' && !confirm('确定删除密钥「' + name + '」？调用方会立刻失效，不可恢复。')) return;
    b.disabled = true;
    fetch('/panel/api/keys/' + act, {method:'POST', headers:hdr(), body: JSON.stringify({name:name})})
      .then(function(r){ return r.json().then(function(j){ return {s:r.status, j:j}; }); })
      .then(function(x){
        if(!x.j.ok){ throw new Error(x.j.error || ('HTTP ' + x.s)); }
        say('listMsg','ok', act === 'toggle' ? ('已' + (x.j.enabled?'启用':'停用') + '「' + name + '」') : ('已删除「' + name + '」'));
        load();
      })
      .catch(function(e){ say('listMsg','err', e.message); b.disabled = false; });
  });

  el('btnCreate').addEventListener('click', function(){
    var name = el('newName').value.trim();
    var note = el('newNote').value.trim();
    if(!name){ say('createMsg','err','请先填标识'); return; }
    var btn = this;
    btn.disabled = true;
    fetch('/panel/api/keys', {method:'POST', headers:hdr(), body: JSON.stringify({name:name, note:note})})
      .then(function(r){ return r.json().then(function(j){ return {s:r.status, j:j}; }); })
      .then(function(x){
        if(!x.j.ok){ throw new Error(x.j.error || ('HTTP ' + x.s)); }
        el('newName').value = '';
        el('newNote').value = '';
        var e = el('createMsg');
        e.className = 'msg show ok';
        e.innerHTML = '已签发「' + esc(name) + '」。<b>完整密钥只显示这一次</b>，请立刻复制：'
          + '<div class="secret"><code>' + esc(x.j.secret) + '</code>'
          + '<button class="xs" id="btnCopy">复制</button></div>';
        el('btnCopy').onclick = function(){
          var self = this;
          navigator.clipboard.writeText(x.j.secret).then(function(){ self.textContent = '已复制'; }, function(){ self.textContent = '复制失败，请手动选中'; });
        };
        btn.disabled = false;
        load();
      })
      .catch(function(e){ say('createMsg','err', e.message); btn.disabled = false; });
  });

  el('btnReveal').addEventListener('click', function(){
    revealed = !revealed;
    this.textContent = revealed ? '隐藏完整密钥' : '显示完整密钥';
    load();
  });
  el('btnReload').addEventListener('click', load);

  load();
})();
