import { createAPI } from '/web/api.js';
import { installAccounts } from '/web/accounts.js';
const $=id=>document.getElementById(id);let restoreData,loaded='';
function show(value){$('result').textContent=typeof value==='string'?value:JSON.stringify(value,null,2)}
function status(value){$('status').textContent=value}
const { api, cancel } = createAPI(() => $('token').value, status);
function parsed(text){try{return JSON.parse(text)}catch{return text||'操作完成'}}
function save(blob,name){const link=document.createElement('a');link.href=URL.createObjectURL(blob);link.download=name;link.click();setTimeout(()=>URL.revokeObjectURL(link.href),1000)}
async function load(){const data=JSON.parse(await api('/api/config'));loaded=JSON.stringify(data,null,2);$('config').value=loaded;$('output').replaceChildren();for(const output of data.outputs){const option=document.createElement('option');option.value=output.id;option.textContent=output.name+' · '+output.id;$('output').append(option)}show({version:data.version,revision:data.revision,sources:data.sources.length,outputs:data.outputs.length})}
async function action(operation,extra={}){const result=await api('/api/action',{operation,...extra});show(parsed(result));if(['grant','revoke','archive','restore','refresh'].includes(operation))status('操作完成。下次编辑前请重新载入最新配置版本。')}
function bind(id,fn){$(id).onclick=()=>Promise.resolve().then(fn).catch(error=>{status(error.name==='AbortError'?'请求已取消':error.message);show(error.message)})}
const clearAccounts = installAccounts({ $, api, bind, show, save });
bind('connect',load);bind('reload',load);bind('disconnect',()=>{cancel();clearAccounts();$('token').value='';$('config').value='';$('password').value='';restoreData=null;$('restoreApply').disabled=true;$('output').replaceChildren();show('已断开');status('已清除页面内令牌与配置')});
bind('preview',async()=>{const data=parsed(await api('/api/preview',{config:JSON.parse($('config').value)}));show(data)});
bind('apply',async()=>{await api('/api/apply',{config:JSON.parse($('config').value)});await load();status('配置已发布')});
bind('download',async()=>save(await api('/api/export?id='+encodeURIComponent($('output').value),undefined,true),$('output').value+'.txt'));
bind('check',()=>action('check',{id:$('output').value,probe_url:$('probe').value}));bind('cancel',()=>cancel());
bind('grant',()=>action('grant',{id:$('grantID').value,outputs:$('grantOutputs').value,expires:$('expires').value,max_downloads:Number($('count').value)}));bind('revoke',()=>action('revoke',{id:$('grantID').value}));bind('refresh',()=>action('refresh'));bind('deliver',()=>action('deliver',{id:$('deliveryID').value}));bind('archive',()=>action('archive',{id:$('objectID').value,kind:$('kind').value}));bind('restore',()=>action('restore',{id:$('objectID').value}));
for(const id of ['logs','clients','trash'])bind(id,async()=>show(parsed(await api('/api/'+id))));
bind('backupDownload',async()=>save(await api('/api/backup',{password:$('password').value},true),'subscriptions.sbvpn'));
async function backupBody(){const file=$('backupFile').files[0];if(!file)throw Error('请选择备份文件');if(file.size>8*1024*1024+52)throw Error('备份文件过大');const data=new Uint8Array(await file.arrayBuffer());let text='';for(let i=0;i<data.length;i+=32768)text+=String.fromCharCode(...data.subarray(i,i+32768));return {password:$('password').value,backup:btoa(text)}}
bind('restorePreview',async()=>{restoreData=null;$('restoreApply').disabled=true;const candidate=await backupBody();show(parsed(await api('/api/restore-backup',candidate)));restoreData=candidate;$('restoreApply').disabled=false});
bind('restoreApply',async()=>{if(!restoreData)throw Error('请先预览备份');show(parsed(await api('/api/restore-backup',{...restoreData,apply:true})));restoreData=null;$('restoreApply').disabled=true;status('恢复完成，分享已关闭；请在终端重新启用订阅')});
for(const id of ['backupFile','password'])$(id).onchange=()=>{restoreData=null;$('restoreApply').disabled=true};
for(const button of document.querySelectorAll('[data-tab]'))button.onclick=()=>{for(const tab of document.querySelectorAll('[data-tab]'))tab.setAttribute('aria-selected',String(tab===button));for(const pane of document.querySelectorAll('.pane'))pane.hidden=pane.id!==button.dataset.tab};
window.addEventListener('beforeunload', event => { if (loaded && $('config').value !== loaded) { event.preventDefault(); event.returnValue = ''; } });
