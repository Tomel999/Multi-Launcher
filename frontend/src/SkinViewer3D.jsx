import { useEffect, useRef } from "preact/hooks";
import { SkinViewer } from "skinview3d";
export function SkinViewer3D({ skinUrl, mini, model }) {
  const mountRef = useRef(null);
  useEffect(() => {
    if (!skinUrl) return;
    const mount = mountRef.current;
    if (!mount) return;
    const size = mini ? { width: 72, height: 88 } : { width: mount.clientWidth || 220, height: mount.clientHeight || 280 };
    const viewer = new SkinViewer({
      canvas: document.createElement("canvas"),
      width: size.width,
      height: size.height,
      skin: skinUrl,
      model: model || "auto-detect"
    });
    mount.appendChild(viewer.canvas);
    viewer.autoRotate = false;
    viewer.zoom = 0.8;
    viewer.fov = 55;
    if (mini) {
      return () => {
        viewer.dispose();
        viewer.canvas.remove();
      };
    }
    const ro = new ResizeObserver(() => {
      const w = mount.clientWidth, h = mount.clientHeight;
      if (!w || !h) return;
      viewer.width = w;
      viewer.height = h;
    });
    ro.observe(mount);
    return () => {
      ro.disconnect();
      viewer.dispose();
      viewer.canvas.remove();
    };
  }, [skinUrl, mini, model]);
  return <div ref={mountRef} className={mini ? "skin-3d-mini" : "skin-3d"} />;
}
