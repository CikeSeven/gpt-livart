import React from 'react';
import { Github } from 'lucide-react';

const PROJECT_LINKS = {
  github: 'https://github.com/CikeSeven/gpt-livart'
};

interface ProjectLinksProps {
  className?: string;
}

const ProjectLinks: React.FC<ProjectLinksProps> = ({ className = '' }) => (
  <div className={`flex items-center gap-1.5 rounded-2xl border border-gray-100 bg-white/90 p-1 backdrop-blur-2xl ${className}`}>
    <a
      href={PROJECT_LINKS.github}
      target="_blank"
      rel="noreferrer"
      className="flex h-9 w-9 items-center justify-center rounded-xl text-gray-500 transition-all hover:bg-gray-100 hover:text-gray-900 active:scale-95"
      title="打开 GitHub 项目主页"
      aria-label="打开 GitHub 项目主页"
    >
      <Github size={19} strokeWidth={2.3} />
    </a>
  </div>
);

export default ProjectLinks;
