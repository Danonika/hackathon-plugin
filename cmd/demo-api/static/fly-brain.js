import * as THREE from 'three';
import { OBJLoader } from 'three/addons/loaders/OBJLoader.js';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js';

const DEFAULT_KC_TOTAL = 4133;
const MAX_SNAPSHOT_AGE_MS = 5000;
const REGION_FILES = ['medulla.obj', 'saddle_FBbt_00045048.obj'];

const clamp = (value, min, max) => Math.min(max, Math.max(min, value));
const finite = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;

function timestampMs(value) {
  if (typeof value === 'number') return value < 1e12 ? value * 1000 : value;
  if (typeof value !== 'string' || !value.trim()) return NaN;
  return Date.parse(value);
}

// Positions illustrate KC identities, not reconstructed KC anatomy. This low
// discrepancy distribution is stable across reloads and uses no random spikes.
function neuronPosition(index, count, array) {
  const side = index % 2 === 0 ? -1 : 1;
  const ordinal = Math.floor(index / 2);
  const perSide = Math.ceil(count / 2);
  const u = (ordinal + 0.5) / perSide;
  const angle = ordinal * 2.399963229728653;
  const elevation = 1 - 2 * ((ordinal * 0.618033988749895 + 0.23) % 1);
  const radius = Math.cbrt(u);
  const ring = Math.sqrt(1 - elevation * elevation);
  const localX = Math.cos(angle) * ring * radius;
  const localY = elevation * radius;
  const localZ = Math.sin(angle) * ring * radius;
  // Bilateral, slightly curved envelopes; individual regions retain their own
  // true VFB alignment. These points deliberately do not claim atlas positions.
  array[index * 3] = side * (0.54 + 0.71 * localX);
  array[index * 3 + 1] = 0.12 + 0.48 * localY + 0.09 * localX * localX;
  array[index * 3 + 2] = 0.40 * localZ + 0.03;
}

/**
 * Independent corner view; the caller owns its one shared animation loop.
 * update(recommendation) accepts the /fly-neuron-events recommend record.
 * render(timeSeconds, dtSeconds, {mode, activity, pulse}) accepts a signed
 * feedback pulse (-1..1), positive for reward. Pulse affects the anatomy rim,
 * never KC activity. No timer, fetch polling or simulated firing runs here.
 */
export async function createBrainView(container) {
  if (!container || typeof container.appendChild !== 'function') {
    throw new TypeError('Для мозга нужен DOM-контейнер.');
  }

  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'low-power' });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
  renderer.setClearColor(0x090916, 0);
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.2;
  renderer.domElement.style.cssText = 'display:block;width:100%;height:100%;touch-action:none;outline:none';
  renderer.domElement.setAttribute('role', 'img');
  renderer.domElement.setAttribute('aria-label', 'Регионы мозга VFB и схема активности KC-нейронов');
  container.appendChild(renderer.domElement);

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(33, 1, 0.01, 40);
  const composer = new EffectComposer(renderer);
  composer.addPass(new RenderPass(scene, camera));
  const bloomPass = new UnrealBloomPass(new THREE.Vector2(1, 1), 1.5, 0.8, 0.35);
  composer.addPass(bloomPass);

  const brain = new THREE.Group();
  brain.rotation.set(-0.23, 0.07, -0.035);
  scene.add(brain);

  const anatomy = new THREE.Group();
  brain.add(anatomy);
  const pulseShell = new THREE.Mesh(
    new THREE.SphereGeometry(2.7, 32, 24),
    new THREE.MeshBasicMaterial({ color: 0x2bff9e, transparent: true, opacity: 0, depthWrite: false, blending: THREE.AdditiveBlending })
  );
  brain.add(pulseShell);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.09;
  controls.enablePan = false;
  controls.enableZoom = false;
  controls.rotateSpeed = 0.55;
  controls.autoRotate = false;
  controls.minPolarAngle = 0.25;
  controls.maxPolarAngle = Math.PI - 0.25;

  scene.add(new THREE.HemisphereLight(0xe8ddff, 0x221329, 2.1));
  const key = new THREE.DirectionalLight(0xffd3e9, 2.7);
  key.position.set(-2, 3, 5);
  scene.add(key);
  const rim = new THREE.DirectionalLight(0x9f8bff, 2.3);
  rim.position.set(4, 1, -3);
  scene.add(rim);

  const regionMaterials = [
    new THREE.MeshPhysicalMaterial({
      color: 0xdcb7dd, emissive: 0x4e245c, emissiveIntensity: 0.12,
      roughness: 0.35, metalness: 0.05, transparent: true, opacity: 0.27,
      side: THREE.DoubleSide, depthWrite: false,
      clearcoat: 0.55, clearcoatRoughness: 0.28,
    }),
    new THREE.MeshPhysicalMaterial({
      color: 0xb8a1f6, emissive: 0x47205e, emissiveIntensity: 0.14,
      roughness: 0.38, metalness: 0.03, transparent: true, opacity: 0.46,
      side: THREE.DoubleSide, depthWrite: false,
      clearcoat: 0.4, clearcoatRoughness: 0.3,
    }),
  ];

  const loader = new OBJLoader();
  // Settle both requests before cleanup so a late successful request cannot
  // leave geometry attached to a failed, disposed view.
  const loaded = await Promise.allSettled(REGION_FILES.map(file => loader.loadAsync(`/vfb-mesh/${file}`)));
  for (const result of loaded) {
    if (result.status === 'fulfilled') anatomy.add(result.value);
  }
  const failureIndex = loaded.findIndex(result => result.status === 'rejected');
  if (failureIndex !== -1) {
    anatomy.traverse(node => { if (node.isMesh) node.geometry.dispose(); });
    regionMaterials.forEach(material => material.dispose());
    controls.dispose();
    renderer.dispose();
    renderer.domElement.remove();
    throw new Error(`Не удалось загрузить регион мозга ${REGION_FILES[failureIndex]}. Проверьте /vfb-mesh/.`, { cause: loaded[failureIndex].reason });
  }

  loaded.forEach((result, index) => {
    result.value.traverse(node => {
      if (!node.isMesh) return;
      const oldMaterials = Array.isArray(node.material) ? node.material : [node.material];
      oldMaterials.forEach(material => material?.dispose());
      if (!node.geometry.getAttribute('normal')) node.geometry.computeVertexNormals();
      node.material = regionMaterials[index];
      node.renderOrder = index + 1;
    });
  });
  // One union bounding box and one transform: never center/scale each region
  // independently, which would incorrectly stack the saddle inside the medulla.
  const box = new THREE.Box3().setFromObject(anatomy);
  const size = box.getSize(new THREE.Vector3());
  const center = box.getCenter(new THREE.Vector3());
  const scale = 3.8 / Math.max(size.x, size.y, size.z, 0.0001);
  anatomy.scale.setScalar(scale);
  anatomy.position.copy(center).multiplyScalar(-scale);

  const pointMaterial = new THREE.ShaderMaterial({
    transparent: true,
    vertexColors: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
    uniforms: { pixelRatio: { value: renderer.getPixelRatio() } },
    vertexShader: `
      attribute float intensity;
      varying vec3 neuronColor;
      varying float neuronIntensity;
      uniform float pixelRatio;
      void main() {
        neuronColor = color;
        neuronIntensity = intensity;
        vec4 viewPosition = modelViewMatrix * vec4(position, 1.0);
        gl_Position = projectionMatrix * viewPosition;
        float distanceScale = clamp(5.5 / -viewPosition.z, 0.65, 1.4);
        gl_PointSize = (3.2 + 7.5 * sqrt(intensity)) * pixelRatio * distanceScale;
      }
    `,
    fragmentShader: `
      varying vec3 neuronColor;
      varying float neuronIntensity;
      void main() {
        float radius = length(gl_PointCoord - vec2(0.5)) * 2.0;
        if (radius > 1.0) discard;
        float core = exp(-6.0 * radius * radius);
        float halo = pow(1.0 - radius, 1.8) * 0.55 * neuronIntensity;
        float alpha = core * (0.85 + 0.6 * neuronIntensity) + halo * 1.6;
        gl_FragColor = vec4(neuronColor, alpha);
        #include <tonemapping_fragment>
        #include <colorspace_fragment>
      }
    `,
  });
  let points;
  let neuronCount = 0;
  let intensity;
  let colors;
  let hasVisibleActivation = false;
  let disposed = false;
  let resizeObserver;
  let lastUpdateAt = -Infinity;
  const status = {
    kcTotal: DEFAULT_KC_TOTAL, activeCount: 0, available: false,
    complete: false, fresh: false, eventId: null, timestamp: null,
  };

  function clearActivity() {
    if (!points || !hasVisibleActivation) return;
    intensity.fill(0);
    for (let index = 0; index < neuronCount; index++) {
      colors[index * 3] = 0.19;
      colors[index * 3 + 1] = 0.14;
      colors[index * 3 + 2] = 0.32;
    }
    points.geometry.attributes.intensity.needsUpdate = true;
    points.geometry.attributes.color.needsUpdate = true;
    hasVisibleActivation = false;
    status.activeCount = 0;
  }

  function setNeuronCount(count) {
    if (count === neuronCount) return;
    if (points) { brain.remove(points); points.geometry.dispose(); }
    neuronCount = count;
    const positions = new Float32Array(count * 3);
    colors = new Float32Array(count * 3);
    intensity = new Float32Array(count);
    for (let index = 0; index < count; index++) neuronPosition(index, count, positions);
    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3).setUsage(THREE.DynamicDrawUsage));
    geometry.setAttribute('intensity', new THREE.BufferAttribute(intensity, 1).setUsage(THREE.DynamicDrawUsage));
    geometry.computeBoundingSphere();
    points = new THREE.Points(geometry, pointMaterial);
    points.name = 'kc-neurons-indexed';
    points.renderOrder = 3;
    brain.add(points);
    hasVisibleActivation = true;
    clearActivity();
    status.kcTotal = count;
  }

  function isFresh() {
    const now = Date.now();
    const age = now - status.timestamp;
    return status.timestamp !== null && age >= -1000 && age <= MAX_SNAPSHOT_AGE_MS
      && now - lastUpdateAt <= MAX_SNAPSHOT_AGE_MS;
  }

  function update(snapshot) {
    if (disposed) return getStatus();
    const recommendation = snapshot?.recommendation ?? snapshot;
    const requestedCount = Number(recommendation?.kc_total);
    if (Number.isSafeInteger(requestedCount) && requestedCount > 0 && requestedCount <= 100000) {
      setNeuronCount(requestedCount);
    }
    clearActivity();
    lastUpdateAt = Date.now();
    const timestamp = timestampMs(recommendation?.ts);
    status.timestamp = Number.isFinite(timestamp) ? timestamp : null;
    status.eventId = recommendation?.event_id ?? null;
    status.available = recommendation?.activation_available === true;
    status.complete = recommendation?.activation_complete === true;
    status.fresh = isFresh();
    if (!status.available || !status.fresh || !Array.isArray(recommendation?.active_kc)) return getStatus();

    for (const neuron of recommendation.active_kc) {
      const index = Number(neuron.index);
      const activation = clamp(finite(neuron.activation), 0, 1);
      if (!Number.isSafeInteger(index) || index < 0 || index >= neuronCount || activation <= 0) continue;
      // Preserve the actual identity. No modulo, sampling or aliasing of IDs.
      if (intensity[index] === 0) status.activeCount++;
      intensity[index] = Math.max(intensity[index], activation);
      const strength = Math.sqrt(intensity[index]);
      colors[index * 3] = 0.19 + strength * 0.91;
      colors[index * 3 + 1] = 0.14 + strength * 0.63;
      colors[index * 3 + 2] = 0.32 + strength * 0.84;
      hasVisibleActivation = true;
    }
    points.geometry.attributes.intensity.needsUpdate = true;
    points.geometry.attributes.color.needsUpdate = true;
    return getStatus();
  }

  function getStatus() {
    status.fresh = isFresh();
    return { ...status, activeCount: status.fresh ? status.activeCount : 0 };
  }

  function resize() {
    if (disposed) return;
    const width = Math.max(container.clientWidth, 1);
    const height = Math.max(container.clientHeight, 1);
    camera.aspect = width / height;
    // Fit the 3.8-unit anatomy with a quiet margin, including narrow layouts.
    const distance = Math.max(4.9, 2.16 / (Math.tan(THREE.MathUtils.degToRad(camera.fov / 2)) * camera.aspect));
    const direction = camera.position.clone().sub(controls.target);
    if (direction.lengthSq() < 0.001) direction.set(0, 0.52, 1);
    camera.position.copy(direction.normalize().multiplyScalar(distance).add(controls.target));
    camera.updateProjectionMatrix();
    renderer.setSize(width, height, false);
    composer.setSize(width, height);
    controls.update();
  }

  function render(time, dt, shared = {}) {
    if (disposed) return;
    status.fresh = isFresh();
    if (!status.fresh) clearActivity();
    if (points && neuronCount > 0) {
      const sparkSeed = Math.floor(time * 6);
      for (let i = 0; i < neuronCount; i++) {
        intensity[i] *= 0.96;
        if (intensity[i] < 0.02) intensity[i] = 0;
        const hash = ((i * 2654435761) ^ (sparkSeed * 40503)) >>> 0;
        if (hash % 997 < 12) intensity[i] = Math.max(intensity[i], 0.75);
      }
      for (let i = 0; i < neuronCount; i++) {
        const s = Math.sqrt(intensity[i]);
        const tint = (i % 5) / 5;
        colors[i * 3] = 0.40 + s * 0.6 - tint * 0.16;
        colors[i * 3 + 1] = 0.34 + s * 0.52 + tint * 0.14;
        colors[i * 3 + 2] = 0.80 + s * 0.2 - tint * 0.06;
      }
      points.geometry.attributes.intensity.needsUpdate = true;
      points.geometry.attributes.color.needsUpdate = true;
    }
    const pulse = clamp(finite(shared.pulse), -1, 1);
    // The shared feedback pulse is visible in the anatomy only. KC points are
    // driven solely by the current recommendation's activation vector.
    const magnitude = Math.abs(pulse);
    const pulseColor = pulse < 0 ? 0xff3b5c : 0x2bff9e;
    regionMaterials[0].emissive.setHex(pulseColor);
    regionMaterials[1].emissive.setHex(pulseColor);
    regionMaterials[0].emissiveIntensity = 0.12 + magnitude * 1.2;
    regionMaterials[1].emissiveIntensity = 0.14 + magnitude * 1.8;
    pulseShell.material.color.setHex(pulseColor);
    pulseShell.material.opacity = magnitude * 0.3;
    pulseShell.scale.setScalar(0.85 + magnitude * 0.5);
    if (colors && points) {
      if (magnitude > 0.02) {
        const targetR = pulse < 0 ? 0.98 : 0.16;
        const targetG = pulse < 0 ? 0.24 : 0.98;
        const targetB = pulse < 0 ? 0.36 : 0.66;
        const k = Math.min(1, magnitude * 0.95);
        for (let i = 0; i < neuronCount; i++) {
          colors[i * 3] += (targetR - colors[i * 3]) * k;
          colors[i * 3 + 1] += (targetG - colors[i * 3 + 1]) * k;
          colors[i * 3 + 2] += (targetB - colors[i * 3 + 2]) * k;
        }
        points.geometry.attributes.color.needsUpdate = true;
      } else if (hasVisibleActivation) {
        for (let i = 0; i < neuronCount; i++) {
          colors[i * 3] += (0.19 - colors[i * 3]) * 0.08;
          colors[i * 3 + 1] += (0.14 - colors[i * 3 + 1]) * 0.08;
          colors[i * 3 + 2] += (0.32 - colors[i * 3 + 2]) * 0.08;
        }
        points.geometry.attributes.color.needsUpdate = true;
      }
    }
    if (magnitude > 0.02 && neuronCount > 0) {
      const probe = new Float32Array(3);
      const wave = (1 - magnitude) * 3.4;
      const band = 0.3;
      for (let i = 0; i < neuronCount; i++) {
        neuronPosition(i, neuronCount, probe);
        const distance = Math.sqrt(probe[0] * probe[0] + probe[1] * probe[1] + probe[2] * probe[2]);
        const offset = Math.abs(distance - wave);
        if (offset < band) {
          const strength = Math.max(intensity[i], 1 - offset / band);
          intensity[i] = strength;
          const s = Math.sqrt(strength);
          colors[i * 3] = 0.19 + s * (pulse < 0 ? 0.79 : 0.05);
          colors[i * 3 + 1] = 0.14 + s * (pulse < 0 ? 0.16 : 0.84);
          colors[i * 3 + 2] = 0.32 + s * (pulse < 0 ? 0.22 : 0.36);
        }
      }
      points.geometry.attributes.intensity.needsUpdate = true;
      points.geometry.attributes.color.needsUpdate = true;
    }
    controls.update();
    composer.render();
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    resizeObserver?.disconnect();
    controls.dispose();
    points?.geometry.dispose();
    pointMaterial.dispose();
    anatomy.traverse(node => { if (node.isMesh) node.geometry.dispose(); });
    regionMaterials.forEach(material => material.dispose());
    renderer.dispose();
    renderer.domElement.remove();
  }

  setNeuronCount(DEFAULT_KC_TOTAL);
  camera.position.set(0, 0.35, 5.6);
  controls.target.set(0, 0, 0);
  resize();
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(container);
  }
  return { update, render, resize, dispose, getStatus, renderer, scene, camera };
}
