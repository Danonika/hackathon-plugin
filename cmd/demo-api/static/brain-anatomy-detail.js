import * as THREE from 'three';

// Illustrative tissue gives context around the two available VFB regions.
// These surfaces, fibers and signal markers are decorative anatomy, never KC data.
export function createDashboardAnatomy(brain, anatomy, regionMaterials) {
  const geometry = new Set();
  const materials = new Set();
  const group = new THREE.Group();
  group.name = 'schematic-neuropil-context';
  brain.add(group);
  const signals = [];
  const ownGeometry = item => (geometry.add(item), item);
  const ownMaterial = item => (materials.add(item), item);
  const vertexShader = `
    varying vec3 vNormal;
    varying vec3 vView;
    varying vec3 vTissue;
    void main() {
      vec4 world = modelMatrix * vec4(position, 1.0);
      vec4 view = viewMatrix * world;
      vNormal = normalize(normalMatrix * normal);
      vView = normalize(-view.xyz);
      vTissue = world.xyz;
      gl_Position = projectionMatrix * view;
    }`;
  function tissueMaterial(tint, opacity, detail = 1) {
    return ownMaterial(new THREE.ShaderMaterial({
      uniforms: { tint: { value: new THREE.Color(tint) }, opacity: { value: opacity }, detail: { value: detail } },
      vertexShader,
      fragmentShader: `
        uniform vec3 tint;
        uniform float opacity;
        uniform float detail;
        varying vec3 vNormal;
        varying vec3 vView;
        varying vec3 vTissue;
        void main() {
          vec3 n = normalize(vNormal);
          float facing = abs(dot(n, normalize(vView)));
          float rim = pow(1.0 - facing, 1.8);
          vec3 p = vTissue;
          float folds = sin(p.y * 49.0 + 3.0 * sin(p.x * 19.0) + 2.0 * sin(p.z * 25.0));
          float fibers = pow(0.5 + 0.5 * sin(p.x * 105.0 + 5.0 * sin(p.y * 28.0) + p.z * 47.0), 12.0);
          float light = 0.40 + 0.42 * max(0.0, dot(n, normalize(vec3(-0.4, 0.7, 1.0))));
          float tissueTexture = (folds * 0.055 + fibers * 0.20) * detail;
          vec3 color = tint * (light + tissueTexture + rim * 0.65);
          color += vec3(0.26, 0.28, 0.65) * (rim * 0.33 + fibers * 0.10 * detail);
          gl_FragColor = vec4(color, opacity * (0.52 + rim * 0.46));
          #include <tonemapping_fragment>
          #include <colorspace_fragment>
        }`,
      transparent: true, depthWrite: false, side: THREE.FrontSide,
    }));
  }

  const lateralMaterial = tissueMaterial(0x756bdd, 0.78, 1.0);
  const saddleMaterial = tissueMaterial(0x9584ed, 0.50, 0.7);
  const atlasMeshes = [];
  anatomy.traverse(node => { if (node.isMesh) atlasMeshes.push(node); });
  for (const mesh of atlasMeshes) {
    mesh.material = mesh.material === regionMaterials[0] ? lateralMaterial : saddleMaterial;
    mesh.renderOrder = 2;
    // Sparse, fixed surface granules read as tissue rather than flashing activity.
    const source = mesh.geometry.getAttribute('position');
    const step = Math.max(9, Math.floor(source.count / 3500));
    const positions = [];
    for (let i = 0; i < source.count; i += step) positions.push(source.getX(i), source.getY(i), source.getZ(i));
    const dotsGeometry = ownGeometry(new THREE.BufferGeometry());
    dotsGeometry.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
    const dotsMaterial = ownMaterial(new THREE.PointsMaterial({
      color: 0xb2a4ff, size: 1.0, sizeAttenuation: false,
      transparent: true, opacity: 0.065, depthWrite: false, blending: THREE.AdditiveBlending,
    }));
    const dots = new THREE.Points(dotsGeometry, dotsMaterial);
    dots.name = 'static-atlas-surface-granules';
    dots.renderOrder = 3;
    mesh.add(dots);
  }

  function organicPoint(theta, phi, radii, phase = 0, fold = 0.035) {
    const s = Math.sin(phi), c = Math.cos(phi);
    const ripple = 1 + fold * (
      Math.sin(theta * 12 + Math.sin(phi * 9) * 1.9 + phase) * s * s +
      0.48 * Math.sin(phi * 23 + theta * 5 + phase) * s +
      0.40 * Math.cos(theta * 7 - phi * 13));
    return new THREE.Vector3(
      -radii[0] * Math.cos(theta) * s * ripple,
      radii[1] * c * ripple,
      radii[2] * Math.sin(theta) * s * ripple,
    );
  }
  function lobe(name, center, radii, phase, tint, opacity, fold = 0.035) {
    const meshGeometry = ownGeometry(new THREE.SphereGeometry(1, 96, 64));
    const positions = meshGeometry.attributes.position;
    const uv = meshGeometry.attributes.uv;
    for (let i = 0; i < positions.count; i++) {
      const theta = uv.getX(i) * Math.PI * 2;
      const phi = (1 - uv.getY(i)) * Math.PI;
      const p = organicPoint(theta, phi, radii, phase, fold);
      positions.setXYZ(i, p.x, p.y, p.z);
    }
    meshGeometry.computeVertexNormals();
    const mesh = new THREE.Mesh(meshGeometry, tissueMaterial(tint, opacity));
    mesh.name = name;
    mesh.position.set(...center);
    mesh.renderOrder = 1;
    group.add(mesh);
    const fibers = [];
    // Curves wind across the surface with different phases and bends.
    for (let i = 0; i < 20; i++) {
      let previous;
      for (let j = 0; j <= 88; j++) {
        const t = j / 88;
        const theta = t * Math.PI * 2 + i * 0.39;
        const phi = Math.PI * (0.13 + 0.74 * (i / 20)) + 0.13 * Math.sin(t * 8 + i * 0.72);
        const p = organicPoint(theta, phi, radii, phase, fold).multiplyScalar(1.006);
        if (previous) fibers.push(previous.x, previous.y, previous.z, p.x, p.y, p.z);
        previous = p;
      }
    }
    const fiberGeometry = ownGeometry(new THREE.BufferGeometry());
    fiberGeometry.setAttribute('position', new THREE.Float32BufferAttribute(fibers, 3));
    const fiberMaterial = ownMaterial(new THREE.LineBasicMaterial({
      color: 0x9b8bef, transparent: true, opacity: 0.035, depthWrite: false,
      blending: THREE.AdditiveBlending,
    }));
    const fibersMesh = new THREE.LineSegments(fiberGeometry, fiberMaterial);
    fibersMesh.renderOrder = 2;
    mesh.add(fibersMesh);
    return mesh;
  }

  // Paired superior envelopes and a tapered ventral volume, visibly schematic.
  lobe('left-superior-neuropil', [-0.34, 0.28, -0.17], [0.62, 0.48, 0.38], 0.6, 0x655bc9, 0.48);
  lobe('right-superior-neuropil', [0.34, 0.28, -0.17], [0.62, 0.48, 0.38], 1.1, 0x6c5dce, 0.48);
  lobe('ventral-neuropil', [0, -0.36, -0.02], [0.39, 0.40, 0.31], 1.7, 0x6b69d7, 0.64, 0.05);
  lobe('central-commissure', [0, -0.04, 0.24], [0.56, 0.20, 0.23], 2.8, 0xaa94ff, 0.65, 0.065);

  function fiberBundle(start, end, color, count, bend, opacity) {
    const vertices = [];
    for (let i = 0; i < count; i++) {
      const phase = i * 2.399963;
      const offset = (i / count - 0.5) * 0.12;
      let previous;
      for (let j = 0; j <= 36; j++) {
        const t = j / 36, s = Math.sin(Math.PI * t);
        const p = new THREE.Vector3(
          THREE.MathUtils.lerp(start[0], end[0], t) + Math.sin(phase + t * 8) * 0.013 * s,
          THREE.MathUtils.lerp(start[1], end[1], t) + s * bend + offset + Math.sin(t * 9 + phase) * 0.023 * s,
          THREE.MathUtils.lerp(start[2], end[2], t) + Math.cos(phase) * 0.07 + s * 0.09);
        if (previous) vertices.push(previous.x, previous.y, previous.z, p.x, p.y, p.z);
        previous = p;
      }
    }
    const lineGeometry = ownGeometry(new THREE.BufferGeometry());
    lineGeometry.setAttribute('position', new THREE.Float32BufferAttribute(vertices, 3));
    const lineMaterial = ownMaterial(new THREE.LineBasicMaterial({ color, transparent: true, opacity, depthWrite: false, blending: THREE.AdditiveBlending }));
    const lines = new THREE.LineSegments(lineGeometry, lineMaterial);
    lines.renderOrder = 4;
    group.add(lines);
    return lineMaterial;
  }
  for (const side of [-1, 1]) {
    fiberBundle([side * 1.23, 0.02, 0.05], [side * 0.48, 0.22, 0.24], 0x8d93ff, 30, 0.10, 0.13);
    fiberBundle([side * 0.52, 0.21, 0.27], [side * 0.13, -0.26, 0.26], 0xac97ff, 19, -0.06, 0.18);
  }

  function signalMarker(side, color, name) {
    const marker = new THREE.Group();
    marker.name = 'schematic-' + name;
    marker.position.set(side * 0.66, 0.22, 0.37);
    group.add(marker);
    const haloMaterial = ownMaterial(new THREE.ShaderMaterial({
      uniforms: { color: { value: new THREE.Color(color) }, strength: { value: 0.20 } },
      vertexShader: 'varying vec2 vUv; void main(){vUv=uv;gl_Position=projectionMatrix*modelViewMatrix*vec4(position,1.0);}',
      fragmentShader: 'varying vec2 vUv; uniform vec3 color; uniform float strength; void main(){float r=length(vUv-0.5)*2.0;float a=exp(-5.0*r*r)*(1.0-smoothstep(0.65,1.0,r));gl_FragColor=vec4(color,strength*a);}',
      transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, side: THREE.DoubleSide,
    }));
    const halo = new THREE.Mesh(ownGeometry(new THREE.PlaneGeometry(0.9, 0.9)), haloMaterial);
    marker.add(halo);
    const coreGeometry = ownGeometry(new THREE.SphereGeometry(0.17, 40, 28));
    const p = coreGeometry.attributes.position;
    for (let i = 0; i < p.count; i++) {
      const x = p.getX(i), y = p.getY(i), z = p.getZ(i);
      const modulation = 1 + 0.15 * Math.sin(y * 45 + side) * Math.cos(x * 36) + 0.08 * Math.sin(z * 60 + y * 30);
      p.setXYZ(i, x * modulation, y * modulation * 1.15, z * modulation * 0.8);
    }
    coreGeometry.computeVertexNormals();
    const coreMaterial = ownMaterial(new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.21, depthWrite: false, blending: THREE.AdditiveBlending }));
    marker.add(new THREE.Mesh(coreGeometry, coreMaterial));
    const cluster = [];
    const branches = [];
    for (let i = 0; i < 38; i++) {
      const angle = i * 2.399963;
      const r = 0.055 + 0.105 * Math.sqrt((i + 0.5) / 38);
      const x = Math.cos(angle) * r, y = Math.sin(angle) * r * 1.15, z = Math.sin(i * 1.73) * 0.09 + 0.08;
      cluster.push(x, y, z);
      branches.push(x * 0.20, y * 0.20, 0.03, x, y, z);
    }
    const clusterGeometry = ownGeometry(new THREE.BufferGeometry());
    clusterGeometry.setAttribute('position', new THREE.Float32BufferAttribute(cluster, 3));
    const dotMaterial = ownMaterial(new THREE.PointsMaterial({ color: new THREE.Color(color).lerp(new THREE.Color(0xffffff), 0.30), size: 0.026, transparent: true, opacity: 0.62, depthWrite: false, blending: THREE.AdditiveBlending }));
    marker.add(new THREE.Points(clusterGeometry, dotMaterial));
    const branchGeometry = ownGeometry(new THREE.BufferGeometry());
    branchGeometry.setAttribute('position', new THREE.Float32BufferAttribute(branches, 3));
    const branchMaterial = ownMaterial(new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.35, depthWrite: false, blending: THREE.AdditiveBlending }));
    marker.add(new THREE.LineSegments(branchGeometry, branchMaterial));
    const connection = fiberBundle([side * 0.67, 0.22, 0.38], [side * 0.36, -0.16, 0.39], color, 11, -0.035, 0.19);
    const light = new THREE.PointLight(color, 0.45, 0.9, 2);
    marker.add(light);
    signals.push({ haloMaterial, coreMaterial, dotMaterial, branchMaterial, connection, light, halo });
  }
  signalMarker(-1, 0x39eaff, 'PAM');
  signalMarker(1, 0xff52d5, 'PPL1');

  return {
    render(pam, ppl1, camera) {
      signals.forEach((signal, index) => {
        const strength = THREE.MathUtils.clamp(index ? ppl1 : pam, 0, 1);
        signal.haloMaterial.uniforms.strength.value = 0.28 + strength * 0.36;
        signal.coreMaterial.opacity = 0.20 + strength * 0.12;
        signal.dotMaterial.opacity = 0.64 + strength * 0.24;
        signal.branchMaterial.opacity = 0.30 + strength * 0.26;
        signal.connection.opacity = 0.19 + strength * 0.20;
        signal.light.intensity = 0.3 + strength * 0.8;
        // Keep the soft halo facing the observer while the anatomy rotates.
        camera.getWorldQuaternion(signal.halo.quaternion);
        const inverse = signal.halo.parent.getWorldQuaternion(new THREE.Quaternion()).invert();
        signal.halo.quaternion.premultiply(inverse);
      });
    },
    dispose() {
      group.removeFromParent();
      for (const item of geometry) item.dispose();
      for (const item of materials) item.dispose();
    },
  };
}

