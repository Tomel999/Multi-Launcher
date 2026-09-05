import { useEffect, useRef, useState } from "preact/hooks";
import * as THREE from "three";
import { GetPanorama } from "../wailsjs/go/main/App";
const CUBE_ORDER = [3, 1, 4, 5, 2, 0];
export function PanoramaBackground({ version, mode = "animate" }) {
  const mountRef = useRef(null);
  const [staticSrc, setStaticSrc] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    let cancelled = false;
    let renderer, camera, tex, raf, ro, timer;
    setStaticSrc("");
    setLoading(true);
    if (!version || mode === "off") {
      setLoading(false);
      return;
    }
    GetPanorama(version).then((faces) => {
      if (cancelled) return;
      setLoading(false);
      if (!faces[0]) return;
      if (!faces.every(Boolean)) {
        setStaticSrc(faces[0]);
        return;
      }
      const mount = mountRef.current;
      if (!mount) return;
      const width = mount.clientWidth || 1;
      const height = mount.clientHeight || 1;
      renderer = new THREE.WebGLRenderer({ antialias: true });
      renderer.setSize(width, height);
      mount.appendChild(renderer.domElement);
      const scene = new THREE.Scene();
      camera = new THREE.PerspectiveCamera(75, width / height, 0.1, 1e3);
      tex = new THREE.CubeTextureLoader().load(CUBE_ORDER.map((i) => faces[i]));
      scene.background = tex;
      // THREE.Clock is deprecated since r183 - use THREE.Timer instead.
      const timer = new THREE.Timer();
      if (typeof document !== "undefined") timer.connect(document);
      const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      const frozen = mode === "paused" || reduced;
      const spin = frozen ? 0 : 0.02;
      const sway = frozen ? 0 : 0.05;
      const animate = (timestamp) => {
        raf = requestAnimationFrame(animate);
        timer.update(timestamp);
        const t = timer.getElapsed();
        camera.rotation.y = t * spin;
        camera.rotation.x = Math.sin(t * 0.15) * sway;
        renderer.render(scene, camera);
      };
      animate();
      ro = new ResizeObserver(() => {
        const w = mount.clientWidth;
        const h = mount.clientHeight;
        if (!w || !h) return;
        renderer.setSize(w, h);
        camera.aspect = w / h;
        camera.updateProjectionMatrix();
      });
      ro.observe(mount);
    }).catch(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {
      cancelled = true;
      if (raf) cancelAnimationFrame(raf);
      if (ro) ro.disconnect();
      if (timer) timer.disconnect();
      if (renderer) {
        renderer.dispose();
        renderer.domElement.remove();
      }
      if (tex) tex.dispose();
    };
  }, [version, mode]);
  if (mode === "off") return null;
  return <div className="panorama-container">
            <div ref={mountRef} className="panorama-three" />
            {staticSrc && <div className="panorama-static" style={{ backgroundImage: `url(${staticSrc})` }} />}
            {loading && <div className="panorama-loading">Loading panorama...</div>}
            <div className="panorama-scrim" />
        </div>;
}
