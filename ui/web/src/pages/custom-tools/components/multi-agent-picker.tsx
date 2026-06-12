import { useState } from "react";
import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";
import { useAgents } from "@/pages/agents/hooks/use-agents";

interface MultiAgentPickerProps {
  value: string[];
  onChange: (ids: string[]) => void;
  portalContainer?: React.RefObject<HTMLElement | null>;
}

/**
 * Multi-select agent picker: search + select → badge list.
 * Empty value = global (tool available to all agents).
 */
export function MultiAgentPicker({ value, onChange, portalContainer }: MultiAgentPickerProps) {
  const [inputValue, setInputValue] = useState("");
  const { agents } = useAgents();

  const options = agents
    .filter((a) => !value.includes(a.id))
    .map((a) => ({ value: a.id, label: a.display_name || a.agent_key || a.id }));

  const labelFor = (id: string) => {
    const a = agents.find((ag) => ag.id === id);
    return a ? (a.display_name || a.agent_key || a.id) : id;
  };

  const handleSelect = (selectedId: string) => {
    const trimmed = selectedId.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
    }
    setInputValue("");
  };

  return (
    <div className="space-y-2">
      <Combobox
        value={inputValue}
        onChange={setInputValue}
        onSelect={handleSelect}
        options={options}
        placeholder="Search agents…"
        allowCustom={false}
        portalContainer={portalContainer}
      />
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {value.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1 pr-1">
              {labelFor(id)}
              <button
                type="button"
                onClick={() => onChange(value.filter((v) => v !== id))}
                className="relative ml-0.5 cursor-pointer rounded-full p-0.5 hover:bg-muted after:absolute after:-inset-2 after:content-[''] md:after:hidden"
              >
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}
