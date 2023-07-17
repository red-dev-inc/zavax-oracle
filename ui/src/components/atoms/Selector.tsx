import React, { ChangeEvent } from 'react';

interface Node {
    id: number;
    ip: string;
    node: string;
}

interface SelectorProps {
    nodes: Node[];
    name: string;
    className: string;
    onChange: (e: ChangeEvent<HTMLSelectElement>) => void;
}

const Selector: React.FC<SelectorProps> = ({
    nodes,
    name,
    className,
    onChange
}) => {
    return (
        <select
            className={className}
            onChange={onChange}
            name={name}
            defaultValue=""
        >
            <option key={0} value={""}>Select a node to query</option>
            {nodes.map((node) => (
                <option key={node.id} value={node.ip}>
                    {node.node}
                </option>
            ))}
        </select>
    );
};

export default Selector;
