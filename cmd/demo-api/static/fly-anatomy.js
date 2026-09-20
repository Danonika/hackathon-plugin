import * as THREE from 'three';
import { OBJLoader } from 'three/addons/loaders/OBJLoader.js';

const MODEL_URL = '/fly-body/fruitfly.xml';
const TAU = Math.PI * 2;
const clamp = THREE.MathUtils.clamp;
const finite = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
const children = (element, tag) => Array.from(element.children).filter(child => child.tagName === tag);
const attrs = element => Object.fromEntries(Array.from(element.attributes, attr => [attr.name, attr.value]));

function numbers(value, fallback) {
  if (!value) return fallback.slice();
  const parsed = value.trim().split(/\s+/).map(Number);
  if (parsed.length !== fallback.length || parsed.some(number => !Number.isFinite(number))) {
    throw new Error(`Некорректные координаты модели: ${value}`);
  }
  return parsed;
}

function applyPose(object, values, angleScale) {
  object.position.fromArray(numbers(values.pos, [0, 0, 0]));
  if (values.quat) {
    const [w, x, y, z] = numbers(values.quat, [1, 0, 0, 0]);
    object.quaternion.set(x, y, z, w).normalize();
  } else if (values.euler) {
    const [x, y, z] = numbers(values.euler, [0, 0, 0]).map(value => value * angleScale);
    object.rotation.set(x, y, z, 'XYZ');
  } else if (values.axisangle) {
    const [x, y, z, angle] = numbers(values.axisangle, [0, 0, 1, 0]);
    object.quaternion.setFromAxisAngle(new THREE.Vector3(x, y, z).normalize(), angle * angleScale);
  }
}

// Resolve MJCF classes before reading any meshes: the 0.1 asset scale belongs
// to <default><mesh>, not to a geom or to the whole articulated skeleton.
function readDefaults(document) {
  const classes = new Map([['main', {}]]);
  function walk(element, inherited) {
    const values = { ...inherited };
    for (const child of element.children) {
      if (child.tagName !== 'default') values[child.tagName] = { ...values[child.tagName], ...attrs(child) };
    }
    classes.set(element.getAttribute('class') || 'main', values);
    for (const child of children(element, 'default')) walk(child, values);
  }
  for (const element of children(document.documentElement, 'default')) walk(element, {});
  return (element, inheritedClass = 'main') => {
    const className = element.getAttribute('class') || inheritedClass;
    return { ...classes.get(className)?.[element.tagName], ...attrs(element) };
  };
}

async function fetchText(url) {
  let response;
  try {
    response = await fetch(url);
  } catch (error) {
    throw new Error(`Не удалось загрузить ${url}: ${error.message}`);
  }
  if (!response.ok) throw new Error(`Не удалось загрузить ${url} (HTTP ${response.status})`);
  return response.text();
}

/**
 * Load the local DeepMind/Janelia fruitfly as an articulated Three.js model.
 * The returned group's origin is its rest-pose bounds centre; +X is forward,
 * +Y is up. Position/rotation of that group belong exclusively to the caller.
 */
export async function loadFlyModel() {
  const xml = await fetchText(MODEL_URL);
  const document = new DOMParser().parseFromString(xml, 'application/xml');
  if (document.querySelector('parsererror')) throw new Error('fruitfly.xml содержит ошибку XML');
  const world = document.querySelector('worldbody');
  const asset = document.querySelector('asset');
  if (!world || !asset) throw new Error('В fruitfly.xml отсутствуют worldbody или asset');

  const resolve = readDefaults(document);
  const compiler = document.querySelector('compiler');
  const angleScale = compiler?.getAttribute('angle') === 'degree' ? Math.PI / 180 : 1;
  const meshDirectory = compiler?.getAttribute('meshdir') || '.';
  const baseURL = new URL(MODEL_URL, window.location.href);
  const assetsURL = new URL(`${meshDirectory.replace(/\/$/, '')}/`, baseURL);
  const meshes = new Map();
  const definitions = new Map();
  const materials = new Map();
  for (const element of children(asset, 'mesh')) {
    const values = resolve(element);
    if (!values.name || !values.file) continue;
    meshes.set(values.name, { url: new URL(values.file, assetsURL).href, scale: numbers(values.scale, [1, 1, 1]) });
  }
  for (const element of children(asset, 'material')) definitions.set(element.getAttribute('name'), attrs(element));

  function materialFor(values, bodyName) {
    const name = values.material || 'body';
    const definition = definitions.get(name) || {};
    let rgba = numbers(values.rgba || definition.rgba, [0.674, 0.35, 0.143, 1]);
    const isLeg = /^(coxa|femur|tibia|tarsus|claw)/.test(bodyName);
    const lowerBody = bodyName.toLowerCase();
    if (/wing/.test(lowerBody)) rgba = [0.74, 0.80, 0.87, 0.34];
    else if (/eye/.test(lowerBody)) rgba = [0.45, 0.07, 0.07, 1];
    else if (isLeg) rgba = [0.16, 0.12, 0.10, 1];
    else if (/^(thorax|abdomen|head|antenna|rostrum|haustellum|haltere)/.test(lowerBody)) rgba = [0.21, 0.16, 0.13, 1];
    const tone = name === 'body' ? (isLeg ? 0.59 : /^(head|antenna|rostrum|haustellum)/.test(bodyName) ? 0.75 : 1) : 1;
    const key = `${name}:${rgba.join(',')}:${tone}`;
    if (!materials.has(key)) {
      const membrane = name === 'membrane';
      const shininess = clamp(finite(definition.shininess, 0.6), 0, 1);
      const material = new THREE.MeshPhysicalMaterial({
        name: `fly-${name}`,
        color: new THREE.Color(rgba[0], rgba[1], rgba[2]).multiplyScalar(tone),
        roughness: membrane ? 0.21 : name === 'red' ? 0.28 : 0.83 - shininess * 0.39,
        metalness: 0,
        clearcoat: membrane ? 0.8 : name === 'red' ? 0.42 : 0.12,
        clearcoatRoughness: membrane ? 0.17 : 0.36,
        opacity: rgba[3],
        transparent: rgba[3] < 1,
        depthWrite: rgba[3] >= 1,
        side: membrane ? THREE.DoubleSide : THREE.FrontSide,
      });
      materials.set(key, material);
    }
    return materials.get(key);
  }

  const group = new THREE.Group();
  group.name = 'fruitfly';
  const normalizer = new THREE.Group();
  const skeleton = new THREE.Group();
  skeleton.rotation.x = -Math.PI / 2;
  normalizer.add(skeleton);
  group.add(normalizer);
  const loader = new OBJLoader();
  const meshPromises = new Map();
  const sourceGeometries = new Set();
  const pendingMeshes = [];
  const articulated = [];
  let meshCount = 0;
  let disposed = false;

  function meshSource(url) {
    if (!meshPromises.has(url)) {
      // Cache the promise immediately so even concurrent references share one load.
      meshPromises.set(url, fetchText(url).then(text => {
        const source = loader.parse(text);
        let count = 0;
        source.traverse(node => {
          if (!node.isMesh) return;
          count++;
          sourceGeometries.add(node.geometry);
          if (!node.geometry.getAttribute('normal')) node.geometry.computeVertexNormals();
          const parsedMaterials = Array.isArray(node.material) ? node.material : [node.material];
          parsedMaterials.forEach(material => material.dispose());
        });
        if (!count) throw new Error(`В файле модели нет геометрии: ${url}`);
        return source;
      }));
    }
    return meshPromises.get(url);
  }

  function walk(element, parent, inheritedClass = 'main') {
    for (const body of children(element, 'body')) {
      const values = resolve(body, inheritedClass);
      const bodyClass = values.childclass || inheritedClass;
      const part = new THREE.Group();
      part.name = values.name || '';
      applyPose(part, values, angleScale);
      parent.add(part);
      const joints = children(body, 'joint').map(element => {
        const joint = resolve(element, bodyClass);
        return {
          name: joint.name || '',
          axis: new THREE.Vector3(...numbers(joint.axis, [0, 0, 1])).normalize(),
          range: numbers(joint.range, [-Math.PI, Math.PI]).map(value => value * angleScale),
          angle: 0,
        };
      });
      if (joints.length) articulated.push({ part, base: part.quaternion.clone(), joints });
      for (const geom of children(body, 'geom')) {
        const geometry = resolve(geom, bodyClass);
        if (!geometry.mesh) continue; // Exclude collision proxies, cameras and invisible fluid geoms.
        const descriptor = meshes.get(geometry.mesh);
        if (!descriptor) throw new Error(`Меш ${geometry.mesh} не объявлен в fruitfly.xml`);
        const holder = new THREE.Group();
        holder.name = geometry.name || geometry.mesh;
        applyPose(holder, geometry, angleScale);
        part.add(holder);
        const material = materialFor(geometry, part.name);
        pendingMeshes.push(meshSource(descriptor.url).then(source => {
          const mesh = source.clone(true);
          mesh.scale.fromArray(descriptor.scale);
          mesh.traverse(node => {
            if (!node.isMesh) return;
            node.material = material;
            node.castShadow = !material.transparent;
            node.receiveShadow = !material.transparent;
            node.renderOrder = material.transparent ? 2 : 0;
          });
          holder.add(mesh);
          meshCount++;
        }));
      }
      walk(body, part, bodyClass);
    }
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    sourceGeometries.forEach(geometry => geometry.dispose());
    materials.forEach(material => material.dispose());
    meshPromises.clear();
    group.removeFromParent();
    group.clear();
  }

  try {
    walk(world, skeleton);
    // Settle every in-flight load before disposing on failure: no late GPU leaks.
    const loaded = await Promise.allSettled(pendingMeshes);
    const failure = loaded.find(result => result.status === 'rejected');
    if (failure) throw failure.reason;
    if (!meshCount) throw new Error('В модели мухи нет видимых мешей');
  } catch (error) {
    await Promise.allSettled(pendingMeshes);
    dispose();
    throw error;
  }

  group.updateMatrixWorld(true);
  const bounds = new THREE.Box3().setFromObject(group);
  const centre = bounds.getCenter(new THREE.Vector3());
  const size = bounds.getSize(new THREE.Vector3());
  const scale = 3.6 / Math.max(size.x, size.y, size.z, 1e-6);
  normalizer.scale.setScalar(scale);
  normalizer.position.copy(centre).multiplyScalar(-scale);
  group.userData.forward = [1, 0, 0];
  group.userData.groundOffset = size.y * scale / 2;
  group.userData.meshCount = meshCount;
  group.updateMatrixWorld(true);

  const delta = new THREE.Quaternion();
  let gaitPhase = 0;
  let wingPhase = 0;
  const smooth = { activity: 0, speed: 0, turn: 0, flight: 0 };

  function animate(time, dt, state = {}) {
    if (disposed) return;
    const step = clamp(finite(dt, 1 / 60), 0, 0.1);
    const t = finite(time);
    const damping = 1 - Math.exp(-step * 9);
    const mode = state.mode || 'idle';
    for (const key of Object.keys(smooth)) {
      const target = clamp(finite(state[key]), key === 'turn' ? -1 : 0, 1);
      smooth[key] += (target - smooth[key]) * damping;
    }
    const { activity, speed, turn, flight } = smooth;
    gaitPhase = (gaitPhase + step * (3 + speed * 11)) % TAU;
    // A visible, display-rate wing stroke; this is an animation of motor state,
    // not a claim that the browser resolves the fly's biological wingbeat.
    wingPhase = (wingPhase + step * TAU * (6 + flight * 12)) % TAU;
    const correction = mode === 'correction' ? activity : 0;
    const reward = mode === 'reward' ? activity : 0;

    for (const body of articulated) {
      const name = body.part.name;
      const side = name.endsWith('_right') ? -1 : 1;
      const leg = /_T([123])_/.exec(name);
      const pair = leg ? Number(leg[1]) : 0;
      // Alternating tripod: L1/R2/L3 and R1/L2/R3 move together.
      const phase = gaitPhase + ((pair === 2 ? -side : side) > 0 ? 0 : Math.PI);
      const stride = Math.sin(phase) * speed * (1 - flight * 0.72);
      const lift = Math.max(0, Math.cos(phase)) * speed * (1 - flight * 0.72);
      body.part.quaternion.copy(body.base);

      for (const joint of body.joints) {
        let angle = 0;
        if (name.startsWith('wing_')) {
          if (joint.name.includes('_roll_')) angle = Math.sin(wingPhase) * (0.012 + flight * 0.53 + activity * 0.028);
          if (joint.name.includes('_yaw_')) angle = 0.035 * flight + Math.sin(wingPhase + 0.55) * flight * 0.065;
          if (joint.name.includes('_pitch_')) angle = Math.cos(wingPhase) * flight * 0.12 + turn * side * flight * 0.035;
        } else if (name.startsWith('antenna_')) {
          if (joint.name.includes('abduct')) angle = side * (Math.sin(t * (2.7 + activity) + side) * 0.045 + turn * 0.05);
          else if (joint.name.includes('twist')) angle = Math.sin(t * 3.3 + side) * 0.016;
          else angle = 0.026 + Math.sin(t * 2.1 + side * 0.4) * (0.022 + activity * 0.04) + correction * 0.025;
        } else if (name === 'head') {
          if (joint.name === 'head_abduct') angle = turn * 0.075 + Math.sin(t * 0.7) * 0.015;
          else if (joint.name === 'head') angle = Math.sin(t * 1.3) * 0.015 - reward * 0.03;
        } else if (name.startsWith('coxa_')) {
          if (joint.name.includes('abduct')) angle = stride * 0.08 + turn * side * speed * 0.025;
          else if (!joint.name.includes('twist')) angle = lift * 0.07 + flight * 0.04;
        } else if (name.startsWith('femur_')) {
          if (!joint.name.includes('twist')) angle = stride * 0.11 - flight * 0.055;
        } else if (name.startsWith('tibia_')) {
          angle = -lift * 0.105 + flight * 0.055;
        } else if (name.startsWith('tarsus_')) {
          angle = lift * 0.045;
        } else if (name.startsWith('abdomen')) {
          if (!joint.name.includes('abduct')) angle = Math.sin(t * 2.6) * 0.0025 + flight * 0.0015;
        } else if (name.startsWith('haltere_')) {
          angle = Math.sin(wingPhase + Math.PI / 2) * flight * 0.035;
        } else if (name === 'rostrum' || name === 'haustellum') {
          if (!joint.name.includes('abduct')) angle = reward * 0.035 * (0.75 + Math.sin(t * 4) * 0.25);
        }
        joint.angle += (clamp(angle, joint.range[0], joint.range[1]) - joint.angle) * (name.startsWith('wing_') ? 1 : damping);
        body.part.quaternion.multiply(delta.setFromAxisAngle(joint.axis, joint.angle));
      }
      body.part.quaternion.normalize();
    }
  }

  return { group, animate, meshCount, dispose, groundOffset: group.userData.groundOffset, forward: new THREE.Vector3(1, 0, 0) };
}
