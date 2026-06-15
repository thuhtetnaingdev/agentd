import { Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Dashboard from "./pages/Dashboard";
import Settings from "./pages/Settings";
import Servers from "./pages/Servers";
import Projects from "./pages/Projects";

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="/servers" element={<Servers />} />
        <Route path="/projects" element={<Projects />} />
        <Route path="/projects/:id" element={<Projects />} />
      </Route>
    </Routes>
  );
}
