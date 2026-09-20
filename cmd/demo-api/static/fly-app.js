import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { loadFlyModel } from './fly-anatomy.js';
import { createBrainView } from './fly-brain.js';
import { createLiveState, applySnapshot, advanceVisualState, activeNow } from './fly-state.js';

const $ = id => document.getElementById(id);
const live = createLiveState();
const stage = $('scene');
const motionReduced = matchMedia('(prefers-reduced-motion: reduce)');
const format = new Intl.NumberFormat('ru-RU');
let brain, model, source, fallbackTimer, fallbackPending = false, closing = false;
let lastRevision = null;
const assetIssues = new Set();

function showAssetError(container, text) {
  const error = document.createElement('div');
  error.className = 'asset-error'; error.setAttribute('role','alert');
  const inner = document.createElement('div'); inner.textContent=text;
  const retry=document.createElement('button'); retry.className='error-retry'; retry.textContent='Повторить загрузку';
  retry.addEventListener('click',()=>location.reload());
  inner.append(document.createElement('br'),document.createElement('br'),retry); error.append(inner);container.append(error);
}

function connection(status) {
  live.connection = status;
  $('connection').dataset.state=status;
  $('connection-text').textContent = status==='online'?'Поток подключён':status==='polling'?'Резервное обновление':status==='connecting'?'Подключение':'Восстанавливаем связь';
  $('connection-note').classList.toggle('hidden',status==='online'||status==='connecting');
  $('connection-note').textContent = status==='polling'?'Поток временно недоступен. Данные обновляются раз в секунду.':'Нет связи с сервером. Старые события не воспроизводятся.';
}

function receive(snapshot) {
  if (!snapshot || snapshot.schema_version!==1) throw new Error('Неверный формат потока');
  const changed=applySnapshot(live,snapshot);
  lastRevision=snapshot.revision;
  if (changed && brain) brain.update(live.recommendation);
  renderStatus();
}

function startFallback() {
  if(fallbackTimer || closing) return;
  const poll = async () => {
    if(fallbackPending || closing) return;
    fallbackPending=true;
    try {
      const response=await fetch('/fly-live?snapshot=1',{cache:'no-store',signal:AbortSignal.timeout(5000)});
      if(!response.ok) throw new Error(`HTTP ${response.status}`);
      const snapshot=await response.json();
      if(snapshot.revision!==lastRevision) receive(snapshot);
      if(source?.readyState!==EventSource.OPEN) connection('polling');
    } catch { if(source?.readyState!==EventSource.OPEN) connection('offline'); }
    finally {fallbackPending=false;}
  };
  poll(); fallbackTimer=setInterval(poll,1000);
}
function startLive() {
  if(!('EventSource' in window)){startFallback();return;}
  source=new EventSource('/fly-live');
  source.onopen=()=>{connection('online');clearInterval(fallbackTimer);fallbackTimer=null;};
  source.addEventListener('snapshot',event=>{
    try {receive(JSON.parse(event.data));connection('online');}
    catch {connection('offline');startFallback();}
  });
  source.onerror=()=>{connection('offline');startFallback();};
}

function renderStatus() {
  const now=Date.now(), current=activeNow(live,now);
  const modes={idle:'Ожидание',routing:'Обработка',reward:'Награда',correction:'Коррекция'};
  $('mode-label').textContent=modes[live.mode];
  $('mode-label').style.color=live.mode==='correction'?'#f2b1a8':live.mode==='routing'?'#c4b5fd':'#a4e8cf';
  $('state-description').textContent=live.mode==='routing'?'Новый запрос активировал нейроны. Тело и мозг реагируют на один сигнал.':live.mode==='reward'?'Решение принято. Сигнал награды одновременно меняет реакцию тела и подсветку мозга.':live.mode==='correction'?'Получена коррекция. Муха меняет движение вместе с сигналом обратной связи.':!live.initialized?'Подключаем поток нейронной активности.':!live.recommendation?'Ждём первый запрос к Fly. При появлении события тело и мозг отреагируют вместе.':'Последний сигнал обработан. Новая активность появится при следующем запросе к Fly.';
  $('route').textContent=live.recommendation?.route||'—';
  $('neuron-total').textContent=`${format.format(live.total)} KC`;
  $('active-count').textContent=live.recommendation?(current?format.format(live.activeCount):'0'):'—';
  $('pam-count').textContent=Number.isFinite(live.feedback?.pam)?format.format(live.feedback.pam):'—';
  $('ppl1-count').textContent=Number.isFinite(live.feedback?.ppl1)?format.format(live.feedback.ppl1):'—';
  $('snapshot-status').textContent=!live.recommendation?'Ожидание данных':!live.available?'Активации недоступны':!live.complete?'Неполный снимок':current?'Живой сигнал':'Нет нового сигнала';
  $('activity-label').textContent=current?`${format.format(live.activeCount)} / ${format.format(live.total)} нейронов`:'Нет нового сигнала';
  $('activity-bar').style.width=(current?100*live.activeCount/live.total:0)+'%';
  const latest=[live.recommendation,live.feedback].filter(Boolean).sort((a,b)=>(Date.parse(b.ts)||0)-(Date.parse(a.ts)||0))[0];
  if(latest){
    $('event-label').textContent=latest.type==='feedback'?(latest.reward?'Решение принято':'Решение скорректировано'):'Маршрут выбран';
    const date=new Date(latest.ts);
    $('event-time').textContent=Number.isFinite(date.getTime())?date.toLocaleString('ru-RU',{day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}):'Время события неизвестно';
  }
}

function createScene() {
  const scene=new THREE.Scene();
  const camera=new THREE.PerspectiveCamera(39,1,0.01,80);
  camera.position.set(4.7,2.9,6.7);
  const renderer=new THREE.WebGLRenderer({antialias:true,alpha:true,powerPreference:'high-performance'});
  renderer.setPixelRatio(Math.min(devicePixelRatio,1.75));
  renderer.setClearColor(0,0);
  renderer.toneMapping=THREE.ACESFilmicToneMapping;renderer.toneMappingExposure=1.15;
  renderer.shadowMap.enabled=true;renderer.shadowMap.type=THREE.PCFSoftShadowMap;
  stage.prepend(renderer.domElement);
  const controls=new OrbitControls(camera,renderer.domElement);
  controls.target.set(0,.6,0);controls.enableDamping=true;controls.enablePan=false;
  controls.minDistance=3.7;controls.maxDistance=11;controls.maxPolarAngle=Math.PI*.48;controls.update();
  scene.add(new THREE.HemisphereLight(0xe2e8ff,0x36303c,2));
  const key=new THREE.DirectionalLight(0xffe9d0,3.4);key.position.set(4,7,5);key.castShadow=true;
  key.shadow.mapSize.set(1024,1024);key.shadow.camera.left=-5;key.shadow.camera.right=5;key.shadow.camera.top=5;key.shadow.camera.bottom=-5;key.shadow.normalBias=.035;scene.add(key);
  const rim=new THREE.DirectionalLight(0xa3afff,2.5);rim.position.set(-3,2,-4);scene.add(rim);
  const fill=new THREE.DirectionalLight(0xa7f6da,1);fill.position.set(-4,1,5);scene.add(fill);
  const ground=new THREE.Mesh(new THREE.CircleGeometry(3.5,96),new THREE.MeshStandardMaterial({color:0x181b26,roughness:1,metalness:0,transparent:true,opacity:.65}));ground.rotation.x=-Math.PI/2;ground.receiveShadow=true;ground.position.y=-.025;scene.add(ground);
  for(const radius of [2.5,3.05]){
    const ring=new THREE.Mesh(new THREE.RingGeometry(radius,radius+.008,100),new THREE.MeshBasicMaterial({color:0x74827f,transparent:true,opacity:.17,side:THREE.DoubleSide}));ring.rotation.x=-Math.PI/2;ring.position.y=-.019;scene.add(ring);
  }
  const organism=new THREE.Group();scene.add(organism);
  const views={perspective:[4.7,2.9,6.7],side:[0,1.25,7.7],top:[.01,8,.01]};
  document.querySelectorAll('[data-view]').forEach(button=>button.addEventListener('click',()=>{
    camera.position.set(...views[button.dataset.view]);controls.target.set(0,.6,0);controls.update();
    document.querySelectorAll('[data-view]').forEach(b=>b.setAttribute('aria-pressed',String(b===button)));
  }));
  const resize=()=>{const {width,height}=stage.getBoundingClientRect();if(!width||!height)return;camera.aspect=width/height;camera.updateProjectionMatrix();renderer.setSize(width,height,false);};
  const observer=new ResizeObserver(resize);observer.observe(stage);resize();
  return {scene,camera,renderer,controls,organism,observer};
}

let view;
try {view=createScene();} catch(error){assetIssues.add('body');showAssetError(stage,'Для 3D нужен WebGL. '+error.message);$('model-status').textContent='3D-модель недоступна';}

if(view)loadFlyModel().then(result=>{
  model=result;view.organism.add(model.group);
  $('model-status').textContent=`${model.meshCount} анатомических деталей · потяните, чтобы повернуть`;
}).catch(error=>{assetIssues.add('body');showAssetError(stage,'Не удалось загрузить модель мухи. '+error.message);$('model-status').textContent='Ошибка загрузки модели';});
createBrainView($('brain-view')).then(result=>{brain=result;if(live.recommendation)brain.update(live.recommendation);}).catch(error=>{assetIssues.add('brain');showAssetError($('brain-view'),'Не удалось загрузить мозг. '+error.message);});

startLive();
const statusTimer=setInterval(renderStatus,250);
let previous=performance.now(), elapsed=0;
const target=new THREE.Vector3();
function animate(frameTime) {
  if(closing)return;
  requestAnimationFrame(animate);
  const dt=Math.min((frameTime-previous)/1000,.06);previous=frameTime;
  if(document.hidden)return;
  elapsed+=dt;
  advanceVisualState(live,Date.now(),dt);
  const reduced=motionReduced.matches;
  const energy=reduced?0:live.activity;
  const shared={mode:live.mode,activity:energy,pulse:reduced?0:live.pulse,speed:.05+energy*.35,turn:0,flight:0};
  shared.clap=live.mode==='reward'?Math.max(0,live.pulse):0;
  if(view && model){
    const y=model.groundOffset||.75;
    const amplitude=reduced?0:(live.mode==='idle'?.9:1.15);
    const reaction=live.mode==='correction'?-live.pulse*.22:live.mode==='reward'?live.pulse*.2:0;
    const wander=Math.sin(elapsed*.11)*.5+Math.sin(elapsed*.047+1.3)*.35;
    const pause=Math.max(0,Math.sin(elapsed*.09+2.1))*.6;
    const targetX=Math.sin(elapsed*.25+wander)*amplitude*(1-pause)+reaction;
    const targetZ=Math.sin(elapsed*.34+wander*.7)*amplitude*.4*(1-pause*.5);
    const rearUp=live.mode==='reward'?Math.max(0,live.pulse):0;
    target.set(targetX,y+rearUp*.38,targetZ);
    view.organism.position.lerp(target,1-Math.exp(-dt*4));
    view.organism.rotation.y=reduced?0:Math.sin(elapsed*.23)*.14+reaction+wander*.08;
    view.organism.rotation.x=-0.9*rearUp;
    shared.turn=view.organism.rotation.y;
    shared.speed=live.mode==='reward'?0:.05+energy*.35;
    shared.flight=live.mode==='reward'?.9:live.mode==='correction'?.55:0;
    model.animate(elapsed,dt,shared);
    view.controls.update();view.renderer.render(view.scene,view.camera);
  }else if(view){view.controls.update();view.renderer.render(view.scene,view.camera);}
  brain?.render(elapsed,dt,{...shared,pulse:live.mode==='correction'?-shared.pulse:live.mode==='reward'?shared.pulse:0});
}
requestAnimationFrame(animate);
addEventListener('pagehide',()=>{
  closing=true;source?.close();clearInterval(fallbackTimer);clearInterval(statusTimer);
  view?.observer.disconnect();view?.controls.dispose();model?.dispose?.();brain?.dispose?.();view?.renderer.dispose();
},{once:true});
